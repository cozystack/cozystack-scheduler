package merge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/cozystack/cozystack-scheduler/pkg/apis/v1alpha1"
)

var (
	shared     *merger
	sharedErr  error
	sharedOnce sync.Once
)

var schedulingClassGVR = schema.GroupVersionResource{
	Group:    v1alpha1.Group,
	Version:  v1alpha1.Version,
	Resource: v1alpha1.Resource,
}

type merger struct {
	lister cache.GenericLister
}

// SharedMerger returns a shared ConstraintMerger backed by a dynamic informer
// for SchedulingClass resources. Safe to call from multiple goroutines.
func SharedMerger(ctx context.Context, handle framework.Handle) (ConstraintMerger, error) {
	sharedOnce.Do(func() {
		dynClient, err := dynamic.NewForConfig(handle.KubeConfig())
		if err != nil {
			sharedErr = fmt.Errorf("creating dynamic client for SchedulingClass: %w", err)
			return
		}
		factory := dynamicinformer.NewDynamicSharedInformerFactory(dynClient, 0)
		resource := factory.ForResource(schedulingClassGVR)
		factory.Start(ctx.Done())
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		factory.WaitForCacheSync(waitCtx.Done())
		shared = &merger{lister: resource.Lister()}
	})
	return shared, sharedErr
}

func (m *merger) getSchedulingClass(pod *v1.Pod) (*v1alpha1.SchedulingClass, error) {
	name := pod.Annotations[SchedulingClassAnnotation]
	if name == "" {
		return nil, nil
	}
	obj, err := m.lister.Get(name)
	if err != nil {
		return nil, fmt.Errorf("getting SchedulingClass %q: %w", name, err)
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshaling SchedulingClass %q: %w", name, err)
	}
	sc := &v1alpha1.SchedulingClass{}
	if err := json.Unmarshal(raw, sc); err != nil {
		return nil, fmt.Errorf("unmarshaling SchedulingClass %q: %w", name, err)
	}
	return sc, nil
}

func (m *merger) MergeInterPodAffinity(pod *v1.Pod) (*InterPodAffinityTerms, error) {
	sc, err := m.getSchedulingClass(pod)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, nil
	}

	result := &InterPodAffinityTerms{}
	affinity := pod.Spec.Affinity

	// Parse pod's own required terms
	result.RequiredAffinityTerms, err = framework.GetAffinityTerms(pod, framework.GetPodAffinityTerms(affinity))
	if err != nil {
		return nil, fmt.Errorf("parsing pod affinity terms: %w", err)
	}
	result.RequiredAntiAffinityTerms, err = framework.GetAffinityTerms(pod, framework.GetPodAntiAffinityTerms(affinity))
	if err != nil {
		return nil, fmt.Errorf("parsing pod anti-affinity terms: %w", err)
	}

	// Parse pod's own preferred terms
	if affinity != nil && affinity.PodAffinity != nil {
		result.PreferredAffinityTerms, err = toWeightedAffinityTerms(pod, affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
		if err != nil {
			return nil, fmt.Errorf("parsing pod preferred affinity terms: %w", err)
		}
	}
	if affinity != nil && affinity.PodAntiAffinity != nil {
		result.PreferredAntiAffinityTerms, err = toWeightedAffinityTerms(pod, affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
		if err != nil {
			return nil, fmt.Errorf("parsing pod preferred anti-affinity terms: %w", err)
		}
	}

	// Append CR terms
	if sc.Spec.PodAffinity != nil {
		crTerms, err := framework.GetAffinityTerms(pod, sc.Spec.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
		if err != nil {
			return nil, fmt.Errorf("parsing CR affinity terms: %w", err)
		}
		result.RequiredAffinityTerms = append(result.RequiredAffinityTerms, crTerms...)

		crPref, err := toWeightedAffinityTerms(pod, sc.Spec.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
		if err != nil {
			return nil, fmt.Errorf("parsing CR preferred affinity terms: %w", err)
		}
		result.PreferredAffinityTerms = append(result.PreferredAffinityTerms, crPref...)
	}
	if sc.Spec.PodAntiAffinity != nil {
		crTerms, err := framework.GetAffinityTerms(pod, sc.Spec.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
		if err != nil {
			return nil, fmt.Errorf("parsing CR anti-affinity terms: %w", err)
		}
		result.RequiredAntiAffinityTerms = append(result.RequiredAntiAffinityTerms, crTerms...)

		crPref, err := toWeightedAffinityTerms(pod, sc.Spec.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
		if err != nil {
			return nil, fmt.Errorf("parsing CR preferred anti-affinity terms: %w", err)
		}
		result.PreferredAntiAffinityTerms = append(result.PreferredAntiAffinityTerms, crPref...)
	}

	return result, nil
}

// toWeightedAffinityTerms converts v1.WeightedPodAffinityTerm to framework.WeightedAffinityTerm.
func toWeightedAffinityTerms(pod *v1.Pod, terms []v1.WeightedPodAffinityTerm) ([]framework.WeightedAffinityTerm, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	var result []framework.WeightedAffinityTerm
	for i := range terms {
		parsed, err := framework.GetAffinityTerms(pod, []v1.PodAffinityTerm{terms[i].PodAffinityTerm})
		if err != nil {
			return nil, err
		}
		if len(parsed) > 0 {
			result = append(result, framework.WeightedAffinityTerm{
				AffinityTerm: parsed[0],
				Weight:       terms[i].Weight,
			})
		}
	}
	return result, nil
}

func (m *merger) MergeNodeAffinity(pod *v1.Pod) (*NodeAffinityTerms, error) {
	sc, err := m.getSchedulingClass(pod)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, nil
	}

	result := &NodeAffinityTerms{}

	// Merge NodeSelector maps (CR values override on conflict)
	result.NodeSelector = make(map[string]string)
	for k, v := range pod.Spec.NodeSelector {
		result.NodeSelector[k] = v
	}
	for k, v := range sc.Spec.NodeSelector {
		result.NodeSelector[k] = v
	}

	// Merge Required node affinity (AND semantics via cross-product)
	var podRequired *v1.NodeSelector
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		podRequired = pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	}
	var crRequired *v1.NodeSelector
	if sc.Spec.NodeAffinity != nil {
		crRequired = sc.Spec.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	}
	result.RequiredNodeAffinity = mergeNodeSelectors(podRequired, crRequired)

	// Merge Preferred node affinity (append)
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		result.PreferredNodeAffinity = append(result.PreferredNodeAffinity,
			pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution...)
	}
	if sc.Spec.NodeAffinity != nil {
		result.PreferredNodeAffinity = append(result.PreferredNodeAffinity,
			sc.Spec.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution...)
	}

	return result, nil
}

// mergeNodeSelectors ANDs two NodeSelectors by computing the cross product of their terms.
// Since NodeSelectorTerms are ORed, (A OR B) AND (C OR D) = (AC OR AD OR BC OR BD).
func mergeNodeSelectors(a, b *v1.NodeSelector) *v1.NodeSelector {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	var merged []v1.NodeSelectorTerm
	for _, at := range a.NodeSelectorTerms {
		for _, bt := range b.NodeSelectorTerms {
			exprs := make([]v1.NodeSelectorRequirement, 0, len(at.MatchExpressions)+len(bt.MatchExpressions))
			exprs = append(exprs, at.MatchExpressions...)
			exprs = append(exprs, bt.MatchExpressions...)
			fields := make([]v1.NodeSelectorRequirement, 0, len(at.MatchFields)+len(bt.MatchFields))
			fields = append(fields, at.MatchFields...)
			fields = append(fields, bt.MatchFields...)
			merged = append(merged, v1.NodeSelectorTerm{
				MatchExpressions: exprs,
				MatchFields:      fields,
			})
		}
	}
	return &v1.NodeSelector{NodeSelectorTerms: merged}
}

func (m *merger) MergeTopologySpreadConstraints(pod *v1.Pod) (*TopologySpreadTerms, error) {
	sc, err := m.getSchedulingClass(pod)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, nil
	}

	result := &TopologySpreadTerms{}
	result.Constraints = append(result.Constraints, pod.Spec.TopologySpreadConstraints...)
	result.Constraints = append(result.Constraints, sc.Spec.TopologySpreadConstraints...)
	return result, nil
}
