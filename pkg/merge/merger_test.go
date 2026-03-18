package merge

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cozystack/cozystack-scheduler/pkg/apis/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// fakeLister implements cache.GenericLister backed by a map of name → SchedulingClass.
type fakeLister struct {
	classes map[string]*v1alpha1.SchedulingClass
}

func (f *fakeLister) List(k8slabels.Selector) ([]runtime.Object, error) { return nil, nil }
func (f *fakeLister) ByNamespace(string) cache.GenericNamespaceLister { return nil }
func (f *fakeLister) Get(name string) (runtime.Object, error) {
	sc, ok := f.classes[name]
	if !ok {
		return nil, fmt.Errorf("not found: %s", name)
	}
	raw, _ := json.Marshal(sc)
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(raw, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func newTestMerger(classes map[string]*v1alpha1.SchedulingClass, keys []string) *merger {
	for _, sc := range classes {
		sc.APIVersion = v1alpha1.Group + "/" + v1alpha1.Version
		sc.Kind = "SchedulingClass"
	}
	return &merger{
		lister:            &fakeLister{classes: classes},
		labelSelectorKeys: append([]string(nil), keys...),
	}
}

var allKeys = []string{ApplicationGroupLabel, ApplicationKindLabel, ApplicationNameLabel}

func allLineageLabels() map[string]string {
	return map[string]string{
		ApplicationGroupLabel: "mygroup",
		ApplicationKindLabel:  "mykind",
		ApplicationNameLabel:  "myname",
	}
}

func expectedLineageSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: allLineageLabels()}
}

func TestLineageLabelSelector_AllKeysPresent(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ApplicationGroupLabel: "mygroup",
				ApplicationKindLabel:  "mykind",
				ApplicationNameLabel:  "myname",
			},
		},
	}
	keys := []string{ApplicationGroupLabel, ApplicationKindLabel, ApplicationNameLabel}
	sel := lineageLabelSelector(pod, keys)
	if sel == nil {
		t.Fatal("expected non-nil selector when all keys are present")
	}
	if len(sel.MatchLabels) != 3 {
		t.Fatalf("expected 3 match labels, got %d", len(sel.MatchLabels))
	}
	for _, key := range keys {
		if _, ok := sel.MatchLabels[key]; !ok {
			t.Errorf("expected key %q in match labels", key)
		}
	}
}

func TestLineageLabelSelector_PartialKeys(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ApplicationGroupLabel: "mygroup",
				// Missing kind and name
			},
		},
	}
	keys := []string{ApplicationGroupLabel, ApplicationKindLabel, ApplicationNameLabel}
	sel := lineageLabelSelector(pod, keys)
	if sel != nil {
		t.Fatalf("expected nil selector when not all keys are present, got %v", sel)
	}
}

func TestLineageLabelSelector_NoLabels(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{},
		},
	}
	keys := []string{ApplicationGroupLabel, ApplicationKindLabel, ApplicationNameLabel}
	sel := lineageLabelSelector(pod, keys)
	if sel != nil {
		t.Fatal("expected nil selector when no labels are present")
	}
}

func TestLineageLabelSelector_NilLabels(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{},
	}
	keys := []string{ApplicationGroupLabel}
	sel := lineageLabelSelector(pod, keys)
	if sel != nil {
		t.Fatal("expected nil selector when pod labels are nil")
	}
}

func TestLineageLabelSelector_EmptyKeys(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				ApplicationGroupLabel: "mygroup",
			},
		},
	}
	sel := lineageLabelSelector(pod, nil)
	if sel != nil {
		t.Fatal("expected nil selector when keys are nil")
	}
	sel = lineageLabelSelector(pod, []string{})
	if sel != nil {
		t.Fatal("expected nil selector when keys are empty")
	}
}

func TestDefaultAffinityTermSelectors_NilDefault(t *testing.T) {
	terms := []v1.PodAffinityTerm{
		{TopologyKey: "zone"},
	}
	out := defaultAffinityTermSelectors(terms, nil)
	if len(out) != 1 || out[0].TopologyKey != "zone" {
		t.Fatal("expected original terms returned unchanged")
	}
}

func TestDefaultAffinityTermSelectors_PopulatesNilSelectors(t *testing.T) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}
	existing := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "bar"}}
	terms := []v1.PodAffinityTerm{
		{TopologyKey: "zone", LabelSelector: nil},
		{TopologyKey: "rack", LabelSelector: existing},
	}
	out := defaultAffinityTermSelectors(terms, sel)
	if out[0].LabelSelector == nil {
		t.Fatal("expected nil selector to be populated")
	}
	if out[0].LabelSelector.MatchLabels["app"] != "foo" {
		t.Error("expected populated selector to match default")
	}
	if out[1].LabelSelector.MatchLabels["app"] != "bar" {
		t.Error("expected existing selector values to be preserved")
	}
}

func TestDefaultAffinityTermSelectors_DeepCopy(t *testing.T) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}
	terms := []v1.PodAffinityTerm{
		{TopologyKey: "zone", LabelSelector: nil},
		{TopologyKey: "rack", LabelSelector: nil},
	}
	out := defaultAffinityTermSelectors(terms, sel)
	// Mutate the first assigned selector
	out[0].LabelSelector.MatchLabels["app"] = "mutated"
	// The second should not be affected
	if out[1].LabelSelector.MatchLabels["app"] != "foo" {
		t.Errorf("mutation of one selector affected another: got %q, want %q",
			out[1].LabelSelector.MatchLabels["app"], "foo")
	}
	// The original should not be affected
	if sel.MatchLabels["app"] != "foo" {
		t.Errorf("mutation affected the original selector: got %q, want %q",
			sel.MatchLabels["app"], "foo")
	}
}

func TestDefaultAffinityTermSelectors_DoesNotMutateInput(t *testing.T) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}
	terms := []v1.PodAffinityTerm{
		{TopologyKey: "zone", LabelSelector: nil},
	}
	out := defaultAffinityTermSelectors(terms, sel)
	out[0].TopologyKey = "mutated"
	if terms[0].TopologyKey != "zone" {
		t.Error("output mutation affected the input slice")
	}
}

func TestDefaultWeightedAffinityTermSelectors_NilDefault(t *testing.T) {
	terms := []v1.WeightedPodAffinityTerm{
		{Weight: 10, PodAffinityTerm: v1.PodAffinityTerm{TopologyKey: "zone"}},
	}
	out := defaultWeightedAffinityTermSelectors(terms, nil)
	if len(out) != 1 || out[0].Weight != 10 {
		t.Fatal("expected original terms returned unchanged")
	}
}

func TestDefaultWeightedAffinityTermSelectors_PopulatesNilSelectors(t *testing.T) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}
	existing := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "bar"}}
	terms := []v1.WeightedPodAffinityTerm{
		{Weight: 10, PodAffinityTerm: v1.PodAffinityTerm{TopologyKey: "zone"}},
		{Weight: 20, PodAffinityTerm: v1.PodAffinityTerm{TopologyKey: "rack", LabelSelector: existing}},
	}
	out := defaultWeightedAffinityTermSelectors(terms, sel)
	if out[0].PodAffinityTerm.LabelSelector == nil {
		t.Fatal("expected nil selector to be populated")
	}
	if out[0].PodAffinityTerm.LabelSelector.MatchLabels["app"] != "foo" {
		t.Error("expected populated selector to match default")
	}
	if out[1].PodAffinityTerm.LabelSelector.MatchLabels["app"] != "bar" {
		t.Error("expected existing selector values to be preserved")
	}
}

func TestDefaultWeightedAffinityTermSelectors_DeepCopy(t *testing.T) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}
	terms := []v1.WeightedPodAffinityTerm{
		{Weight: 10, PodAffinityTerm: v1.PodAffinityTerm{TopologyKey: "zone"}},
		{Weight: 20, PodAffinityTerm: v1.PodAffinityTerm{TopologyKey: "rack"}},
	}
	out := defaultWeightedAffinityTermSelectors(terms, sel)
	out[0].PodAffinityTerm.LabelSelector.MatchLabels["app"] = "mutated"
	if out[1].PodAffinityTerm.LabelSelector.MatchLabels["app"] != "foo" {
		t.Errorf("mutation of one selector affected another: got %q, want %q",
			out[1].PodAffinityTerm.LabelSelector.MatchLabels["app"], "foo")
	}
	if sel.MatchLabels["app"] != "foo" {
		t.Errorf("mutation affected the original selector: got %q, want %q",
			sel.MatchLabels["app"], "foo")
	}
}

func TestDeepCopy_MatchLabelsIndependence(t *testing.T) {
	orig := &metav1.LabelSelector{
		MatchLabels: map[string]string{"a": "1", "b": "2"},
	}
	cp := orig.DeepCopy()
	cp.MatchLabels["a"] = "changed"
	if orig.MatchLabels["a"] != "1" {
		t.Error("copy mutation affected original MatchLabels")
	}
}

func TestDeepCopy_MatchExpressionsIndependence(t *testing.T) {
	orig := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "app",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"foo", "bar"},
			},
		},
	}
	cp := orig.DeepCopy()
	cp.MatchExpressions[0].Values[0] = "mutated"
	if orig.MatchExpressions[0].Values[0] != "foo" {
		t.Errorf("copy mutation affected original Values: got %q, want %q",
			orig.MatchExpressions[0].Values[0], "foo")
	}
}

// --- Test: DefaultLabelSelectorKeys capture ---

func TestMergerCapturesLabelSelectorKeys(t *testing.T) {
	keys := []string{"custom.io/key1", "custom.io/key2"}
	m := newTestMerger(nil, keys)
	if len(m.labelSelectorKeys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(m.labelSelectorKeys))
	}
	// Mutate the original slice to verify the merger holds an independent copy
	keys[0] = "mutated"
	if m.labelSelectorKeys[0] != "custom.io/key1" {
		t.Error("merger keys were affected by mutation of the original slice")
	}
}

// --- Test: deep copy of terms prevents NamespaceSelector aliasing ---

func TestDefaultAffinityTermSelectors_DeepCopyNamespaceSelector(t *testing.T) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}
	nsSel := &metav1.LabelSelector{MatchLabels: map[string]string{"ns": "test"}}
	terms := []v1.PodAffinityTerm{
		{TopologyKey: "zone", LabelSelector: nil, NamespaceSelector: nsSel},
	}
	out := defaultAffinityTermSelectors(terms, sel)
	// Mutating the output's NamespaceSelector must not affect the input
	out[0].NamespaceSelector.MatchLabels["ns"] = "mutated"
	if nsSel.MatchLabels["ns"] != "test" {
		t.Error("mutation of output NamespaceSelector affected the input term")
	}
}

func TestDefaultWeightedAffinityTermSelectors_DeepCopyNamespaceSelector(t *testing.T) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}
	nsSel := &metav1.LabelSelector{MatchLabels: map[string]string{"ns": "test"}}
	terms := []v1.WeightedPodAffinityTerm{
		{Weight: 10, PodAffinityTerm: v1.PodAffinityTerm{
			TopologyKey: "zone", LabelSelector: nil, NamespaceSelector: nsSel,
		}},
	}
	out := defaultWeightedAffinityTermSelectors(terms, sel)
	out[0].PodAffinityTerm.NamespaceSelector.MatchLabels["ns"] = "mutated"
	if nsSel.MatchLabels["ns"] != "test" {
		t.Error("mutation of output NamespaceSelector affected the input term")
	}
}

func TestMergeTopologySpread_DeepCopyDoesNotMutateSource(t *testing.T) {
	origSel := &metav1.LabelSelector{MatchLabels: map[string]string{"orig": "val"}}
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: v1.DoNotSchedule,
					LabelSelector:     origSel,
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels:      allLineageLabels(),
		},
	}
	result, err := m.MergeTopologySpreadConstraints(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate the result's LabelSelector
	result.Constraints[0].LabelSelector.MatchLabels["orig"] = "mutated"
	// The original in the SchedulingClass must not be affected
	if origSel.MatchLabels["orig"] != "val" {
		t.Error("mutation of merged constraint affected the source SchedulingClass selector")
	}
}

// --- Integration tests for MergeInterPodAffinity ---

func TestMergeInterPodAffinity_NilSelectorPopulated(t *testing.T) {
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			PodAffinity: &v1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{TopologyKey: "topology.kubernetes.io/zone", LabelSelector: nil},
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels:      allLineageLabels(),
		},
	}
	result, err := m.MergeInterPodAffinity(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.RequiredAffinityTerms) != 1 {
		t.Fatalf("expected 1 required affinity term, got %d", len(result.RequiredAffinityTerms))
	}
	term := result.RequiredAffinityTerms[0]
	// Verify the selector matches the pod's lineage labels
	if !term.Selector.Matches(k8slabels.Set(allLineageLabels())) {
		t.Error("expected selector to match lineage labels")
	}
	// Verify it does NOT match a pod with different labels
	if term.Selector.Matches(k8slabels.Set(map[string]string{"unrelated": "label"})) {
		t.Error("expected selector to NOT match unrelated labels")
	}
}

func TestMergeInterPodAffinity_MissingLabelNoPopulation(t *testing.T) {
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			PodAntiAffinity: &v1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{TopologyKey: "topology.kubernetes.io/zone", LabelSelector: nil},
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels: map[string]string{
				ApplicationGroupLabel: "mygroup",
				// Missing kind and name — selector should NOT be populated
			},
		},
	}
	result, err := m.MergeInterPodAffinity(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.RequiredAntiAffinityTerms) != 1 {
		t.Fatalf("expected 1 required anti-affinity term, got %d", len(result.RequiredAntiAffinityTerms))
	}
	// The nil selector was not populated (incomplete lineage labels).
	// A nil LabelSelector converts to labels.Nothing(), matching no pods.
	term := result.RequiredAntiAffinityTerms[0]
	if term.Selector.Matches(k8slabels.Set(allLineageLabels())) {
		t.Error("expected unpopulated nil-selector term to NOT match lineage labels")
	}
}

func TestMergeInterPodAffinity_ExplicitSelectorPreserved(t *testing.T) {
	explicitSel := &metav1.LabelSelector{MatchLabels: map[string]string{"custom": "selector"}}
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			PodAffinity: &v1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{TopologyKey: "topology.kubernetes.io/zone", LabelSelector: explicitSel},
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels:      allLineageLabels(),
		},
	}
	result, err := m.MergeInterPodAffinity(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.RequiredAffinityTerms) != 1 {
		t.Fatalf("expected 1 required affinity term, got %d", len(result.RequiredAffinityTerms))
	}
	// Verify the explicit selector was preserved — it should match {custom: selector}
	term := result.RequiredAffinityTerms[0]
	if !term.Selector.Matches(k8slabels.Set(map[string]string{"custom": "selector"})) {
		t.Error("expected explicit selector to be preserved")
	}
	// And should NOT match the lineage labels
	if term.Selector.Matches(k8slabels.Set(allLineageLabels())) {
		t.Error("expected explicit selector to NOT match lineage labels")
	}
}

func TestMergeInterPodAffinity_PreferredTermsPopulated(t *testing.T) {
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			PodAffinity: &v1.PodAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
					{Weight: 50, PodAffinityTerm: v1.PodAffinityTerm{
						TopologyKey:   "topology.kubernetes.io/zone",
						LabelSelector: nil,
					}},
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels:      allLineageLabels(),
		},
	}
	result, err := m.MergeInterPodAffinity(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.PreferredAffinityTerms) != 1 {
		t.Fatalf("expected 1 preferred affinity term, got %d", len(result.PreferredAffinityTerms))
	}
	if result.PreferredAffinityTerms[0].Weight != 50 {
		t.Errorf("expected weight 50, got %d", result.PreferredAffinityTerms[0].Weight)
	}
	// Verify the selector was populated from lineage labels
	if !result.PreferredAffinityTerms[0].AffinityTerm.Selector.Matches(k8slabels.Set(allLineageLabels())) {
		t.Error("expected preferred term selector to match lineage labels")
	}
}

func TestMergeInterPodAffinity_NoAnnotation(t *testing.T) {
	m := newTestMerger(nil, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Labels: allLineageLabels()},
	}
	result, err := m.MergeInterPodAffinity(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result when no annotation is present")
	}
}

// --- Integration tests for MergeTopologySpreadConstraints ---

func TestMergeTopologySpread_NilSelectorPopulated(t *testing.T) {
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: v1.DoNotSchedule,
					LabelSelector:     nil,
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels:      allLineageLabels(),
		},
	}
	result, err := m.MergeTopologySpreadConstraints(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(result.Constraints))
	}
	sel := result.Constraints[0].LabelSelector
	if sel == nil {
		t.Fatal("expected LabelSelector to be populated from lineage labels")
	}
	expected := expectedLineageSelector()
	if len(sel.MatchLabels) != len(expected.MatchLabels) {
		t.Fatalf("expected %d match labels, got %d", len(expected.MatchLabels), len(sel.MatchLabels))
	}
	for k, v := range expected.MatchLabels {
		if sel.MatchLabels[k] != v {
			t.Errorf("expected label %q=%q, got %q", k, v, sel.MatchLabels[k])
		}
	}
}

func TestMergeTopologySpread_MissingLabelNoPopulation(t *testing.T) {
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: v1.DoNotSchedule,
					LabelSelector:     nil,
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels: map[string]string{
				ApplicationGroupLabel: "mygroup",
			},
		},
	}
	result, err := m.MergeTopologySpreadConstraints(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Constraints[0].LabelSelector != nil {
		t.Fatal("expected LabelSelector to remain nil when lineage labels are incomplete")
	}
}

func TestMergeTopologySpread_ExplicitSelectorPreserved(t *testing.T) {
	explicitSel := &metav1.LabelSelector{MatchLabels: map[string]string{"custom": "selector"}}
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: v1.DoNotSchedule,
					LabelSelector:     explicitSel,
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels:      allLineageLabels(),
		},
	}
	result, err := m.MergeTopologySpreadConstraints(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sel := result.Constraints[0].LabelSelector
	if sel == nil {
		t.Fatal("expected LabelSelector to be preserved")
	}
	if sel.MatchLabels["custom"] != "selector" {
		t.Errorf("expected explicit selector to be preserved, got %v", sel.MatchLabels)
	}
}

func TestMergeTopologySpread_PodConstraintsMerged(t *testing.T) {
	sc := &v1alpha1.SchedulingClass{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sc"},
		Spec: v1alpha1.SchedulingClassSpec{
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: v1.DoNotSchedule,
					LabelSelector:     nil,
				},
			},
		},
	}
	m := newTestMerger(map[string]*v1alpha1.SchedulingClass{"test-sc": sc}, allKeys)
	podSel := &metav1.LabelSelector{MatchLabels: map[string]string{"pod": "selector"}}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{SchedulingClassAnnotation: "test-sc"},
			Labels:      allLineageLabels(),
		},
		Spec: v1.PodSpec{
			TopologySpreadConstraints: []v1.TopologySpreadConstraint{
				{
					MaxSkew:           2,
					TopologyKey:       "kubernetes.io/hostname",
					WhenUnsatisfiable: v1.ScheduleAnyway,
					LabelSelector:     podSel,
				},
			},
		},
	}
	result, err := m.MergeTopologySpreadConstraints(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Constraints) != 2 {
		t.Fatalf("expected 2 constraints (pod + CR), got %d", len(result.Constraints))
	}
	// First should be the pod's own constraint
	if result.Constraints[0].TopologyKey != "kubernetes.io/hostname" {
		t.Errorf("expected pod constraint first, got topology key %q", result.Constraints[0].TopologyKey)
	}
	// Second should be the CR constraint with populated selector
	if result.Constraints[1].LabelSelector == nil {
		t.Fatal("expected CR constraint selector to be populated")
	}
}
