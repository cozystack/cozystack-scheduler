package merge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/cozystack/cozystack-scheduler/pkg/apis/v1alpha1"
)

// Canonical label keys set by the cozystack lineage webhook to identify an application.
// Mirrored from github.com/cozystack/cozystack/pkg/apis/apps/v1alpha1.
const (
	ApplicationGroupLabel = "apps.cozystack.io/application.group"
	ApplicationKindLabel  = "apps.cozystack.io/application.kind"
	ApplicationNameLabel  = "apps.cozystack.io/application.name"
)

// DefaultLabelSelectorKeys holds the pod label keys used to auto-populate an
// empty LabelSelector in SchedulingClass affinity and topology spread terms.
// It is set at startup via --default-label-selector-keys and captured into the
// merger struct at init time. Do not read after SharedMerger has been called.
var DefaultLabelSelectorKeys []string

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
	lister            cache.GenericLister
	labelSelectorKeys []string
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
		shared = &merger{
			lister:            resource.Lister(),
			labelSelectorKeys: append([]string(nil), DefaultLabelSelectorKeys...),
		}
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

	// Append CR terms, auto-populating empty LabelSelectors from pod lineage labels.
	defaultSelector := lineageLabelSelector(pod, m.labelSelectorKeys)
	if sc.Spec.PodAffinity != nil {
		required := defaultAffinityTermSelectors(sc.Spec.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution, defaultSelector)
		crTerms, err := framework.GetAffinityTerms(pod, required)
		if err != nil {
			return nil, fmt.Errorf("parsing CR affinity terms: %w", err)
		}
		result.RequiredAffinityTerms = append(result.RequiredAffinityTerms, crTerms...)

		preferred := defaultWeightedAffinityTermSelectors(sc.Spec.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution, defaultSelector)
		crPref, err := toWeightedAffinityTerms(pod, preferred)
		if err != nil {
			return nil, fmt.Errorf("parsing CR preferred affinity terms: %w", err)
		}
		result.PreferredAffinityTerms = append(result.PreferredAffinityTerms, crPref...)
	}
	if sc.Spec.PodAntiAffinity != nil {
		required := defaultAffinityTermSelectors(sc.Spec.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, defaultSelector)
		crTerms, err := framework.GetAffinityTerms(pod, required)
		if err != nil {
			return nil, fmt.Errorf("parsing CR anti-affinity terms: %w", err)
		}
		result.RequiredAntiAffinityTerms = append(result.RequiredAntiAffinityTerms, crTerms...)

		preferred := defaultWeightedAffinityTermSelectors(sc.Spec.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution, defaultSelector)
		crPref, err := toWeightedAffinityTerms(pod, preferred)
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

	crConstraints := make([]v1.TopologySpreadConstraint, len(sc.Spec.TopologySpreadConstraints))
	for i := range sc.Spec.TopologySpreadConstraints {
		crConstraints[i] = *sc.Spec.TopologySpreadConstraints[i].DeepCopy()
	}
	defaultSelector := lineageLabelSelector(pod, m.labelSelectorKeys)
	for i := range crConstraints {
		if crConstraints[i].LabelSelector == nil && defaultSelector != nil {
			crConstraints[i].LabelSelector = defaultSelector.DeepCopy()
		}
	}

	result := &TopologySpreadTerms{}
	result.Constraints = append(result.Constraints, pod.Spec.TopologySpreadConstraints...)
	result.Constraints = append(result.Constraints, crConstraints...)
	return result, nil
}

// defaultAffinityTermSelectors returns a deep copy of terms where any nil
// LabelSelector is replaced with a deep copy of defaultSelector.
func defaultAffinityTermSelectors(terms []v1.PodAffinityTerm, defaultSelector *metav1.LabelSelector) []v1.PodAffinityTerm {
	out := make([]v1.PodAffinityTerm, len(terms))
	for i := range terms {
		out[i] = *terms[i].DeepCopy()
		if out[i].LabelSelector == nil && defaultSelector != nil {
			out[i].LabelSelector = defaultSelector.DeepCopy()
		}
	}
	return out
}

// defaultWeightedAffinityTermSelectors returns a deep copy of terms where any
// nil LabelSelector is replaced with a deep copy of defaultSelector.
func defaultWeightedAffinityTermSelectors(terms []v1.WeightedPodAffinityTerm, defaultSelector *metav1.LabelSelector) []v1.WeightedPodAffinityTerm {
	out := make([]v1.WeightedPodAffinityTerm, len(terms))
	for i := range terms {
		out[i] = *terms[i].DeepCopy()
		if out[i].PodAffinityTerm.LabelSelector == nil && defaultSelector != nil {
			out[i].PodAffinityTerm.LabelSelector = defaultSelector.DeepCopy()
		}
	}
	return out
}

// lineageLabelSelector builds a LabelSelector from the pod's labels using the
// configured label selector keys. Returns nil if any of the configured keys is
// missing from the pod's labels — a partial match would produce an overly broad
// selector that could cause unintended co-scheduling.
func lineageLabelSelector(pod *v1.Pod, keys []string) *metav1.LabelSelector {
	if len(keys) == 0 {
		return nil
	}
	matchLabels := make(map[string]string, len(keys))
	for _, key := range keys {
		val, ok := pod.Labels[key]
		if !ok {
			return nil
		}
		matchLabels[key] = val
	}
	return &metav1.LabelSelector{MatchLabels: matchLabels}
}
