/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TeamConditionTypeAccepted = "Accepted"
)

// TeamSpec defines the desired state of Team.
type TeamSpec struct {
	// Each Participant can either be a reference to the name of an Agent in the same namespace as the referencing Team, or a reference to the name of an Agent in a different namespace in the form <namespace>/<name>
	Participants []string `json:"participants"`
	Description  string   `json:"description"`
	// Can either be a reference to the name of a ModelConfig in the same namespace as the referencing Team, or a reference to the name of a ModelConfig in a different namespace in the form <namespace>/<name>
	ModelConfig string `json:"modelConfig"`
	// +kubebuilder:validation:Optional
	RoundRobinTeamConfig *RoundRobinTeamConfig `json:"roundRobinTeamConfig"`
	// +kubebuilder:validation:Optional
	TerminationCondition TerminationCondition `json:"terminationCondition"`
	MaxTurns             int64                `json:"maxTurns"`
}

type RoundRobinTeamConfig struct{}

type TerminationCondition struct {
	// ONEOF: maxMessageTermination, textMentionTermination, orTermination
	MaxMessageTermination       *MaxMessageTermination       `json:"maxMessageTermination,omitempty"`
	TextMentionTermination      *TextMentionTermination      `json:"textMentionTermination,omitempty"`
	TextMessageTermination      *TextMessageTermination      `json:"textMessageTermination,omitempty"`
	FinalTextMessageTermination *FinalTextMessageTermination `json:"finalTextMessageTermination,omitempty"`
	StopMessageTermination      *StopMessageTermination      `json:"stopMessageTermination,omitempty"`
	OrTermination               *OrTermination               `json:"orTermination,omitempty"`
}

type MaxMessageTermination struct {
	MaxMessages int `json:"maxMessages"`
}

type TextMentionTermination struct {
	Text string `json:"text"`
}

type TextMessageTermination struct {
	Source string `json:"source"`
}

type FinalTextMessageTermination struct {
	Source string `json:"source"`
}

type StopMessageTermination struct{}

type OrTermination struct {
	Conditions []OrTerminationCondition `json:"conditions"`
}

type OrTerminationCondition struct {
	MaxMessageTermination  *MaxMessageTermination  `json:"maxMessageTermination,omitempty"`
	TextMentionTermination *TextMentionTermination `json:"textMentionTermination,omitempty"`
}

// TeamStatus defines the observed state of Team.
type TeamStatus struct {
	Conditions         []metav1.Condition `json:"conditions"`
	ObservedGeneration int64              `json:"observedGeneration"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Team is the Schema for the teams API.
type Team struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TeamSpec   `json:"spec,omitempty"`
	Status TeamStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TeamList contains a list of Team.
type TeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Team `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Team{}, &TeamList{})
}

func (t *Team) GetModelConfigName() string {
	return t.Spec.ModelConfig
}
