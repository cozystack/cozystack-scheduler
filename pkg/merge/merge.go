/*
Package merge defines the interface for merging scheduling constraints
from a pod's spec with those defined in a SchedulingClass custom resource.

The flow is:
 1. Read the pod's annotation (e.g. "schedulingclass.cozystack.io/name") to
    identify the SchedulingClass CR.
 2. Fetch the SchedulingClass.cozystack.io/v1alpha1 resource from the cluster.
 3. Merge the CR's constraints with the pod's own spec-level constraints.
 4. Return the merged result for use by the scheduling plugin.
*/
package merge

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// TODO: Define the annotation key used to reference a SchedulingClass CR.
// const SchedulingClassAnnotation = "schedulingclass.cozystack.io/name"

// InterPodAffinityTerms holds the merged set of inter-pod affinity and
// anti-affinity terms for a single pod, combining pod spec and SchedulingClass.
type InterPodAffinityTerms struct {
	RequiredAffinityTerms      []framework.AffinityTerm
	RequiredAntiAffinityTerms  []framework.AffinityTerm
	PreferredAffinityTerms     []framework.WeightedAffinityTerm
	PreferredAntiAffinityTerms []framework.WeightedAffinityTerm
}

// NodeAffinityTerms holds the merged set of node affinity terms for a single
// pod, combining pod spec and SchedulingClass.
type NodeAffinityTerms struct {
	// NodeSelector is the merged result of pod.Spec.NodeSelector and the CR's
	// node selector requirements. It is nil if neither source has any.
	NodeSelector map[string]string

	// RequiredNodeAffinity is the merged node selector from both the pod spec
	// and the SchedulingClass CR.
	RequiredNodeAffinity *v1.NodeSelector

	// PreferredNodeAffinity is the merged list of preferred scheduling terms.
	PreferredNodeAffinity []v1.PreferredSchedulingTerm
}

// TopologySpreadTerms holds the merged set of topology spread constraints for
// a single pod, combining pod spec and SchedulingClass.
type TopologySpreadTerms struct {
	Constraints []v1.TopologySpreadConstraint
}

// ConstraintMerger fetches a SchedulingClass CR for a pod and merges its
// constraints with those already present on the pod spec.
//
// TODO: Implement this interface. The implementation will need:
//   - A dynamic client or typed client for SchedulingClass.cozystack.io/v1alpha1
//   - A lister/informer (with caching) for SchedulingClass resources
//   - Merge semantics: how CR terms combine with pod spec terms (append, override, etc.)
type ConstraintMerger interface {
	// MergeInterPodAffinity returns the merged inter-pod affinity terms for the
	// given pod. It reads the pod's annotation to find the SchedulingClass CR
	// and appends/merges its affinity terms with those from the pod spec.
	// The podInfo argument contains the terms already parsed from the pod spec.
	// If no SchedulingClass annotation is present, it returns the original terms unchanged.
	MergeInterPodAffinity(pod *v1.Pod, podInfo *framework.PodInfo) (*InterPodAffinityTerms, error)

	// MergeNodeAffinity returns the merged node affinity for the given pod.
	// It combines pod.Spec.NodeSelector, pod.Spec.Affinity.NodeAffinity, and
	// the SchedulingClass CR's node affinity terms.
	// If no SchedulingClass annotation is present, it returns nil (use pod spec as-is).
	MergeNodeAffinity(pod *v1.Pod) (*NodeAffinityTerms, error)

	// MergeTopologySpreadConstraints returns the merged topology spread constraints.
	// It appends the CR's constraints to those from pod.Spec.TopologySpreadConstraints.
	// If no SchedulingClass annotation is present, it returns nil (use pod spec as-is).
	MergeTopologySpreadConstraints(pod *v1.Pod) (*TopologySpreadTerms, error)
}
