package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	Group    = "cozystack.io"
	Version  = "v1alpha1"
	Resource = "schedulingclasses"
)

type SchedulingClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SchedulingClassSpec `json:"spec,omitempty"`
}

type SchedulingClassSpec struct {
	NodeSelector              map[string]string             `json:"nodeSelector,omitempty"`
	NodeAffinity              *v1.NodeAffinity              `json:"nodeAffinity,omitempty"`
	PodAffinity               *v1.PodAffinity               `json:"podAffinity,omitempty"`
	PodAntiAffinity           *v1.PodAntiAffinity           `json:"podAntiAffinity,omitempty"`
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}
