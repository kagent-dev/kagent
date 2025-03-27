//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"encoding/json"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Agent) DeepCopyInto(out *Agent) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Agent.
func (in *Agent) DeepCopy() *Agent {
	if in == nil {
		return nil
	}
	out := new(Agent)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Agent) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AgentList) DeepCopyInto(out *AgentList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Agent, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AgentList.
func (in *AgentList) DeepCopy() *AgentList {
	if in == nil {
		return nil
	}
	out := new(AgentList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *AgentList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AgentSpec) DeepCopyInto(out *AgentSpec) {
	*out = *in
	if in.Tools != nil {
		in, out := &in.Tools, &out.Tools
		*out = make([]*Tool, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(Tool)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AgentSpec.
func (in *AgentSpec) DeepCopy() *AgentSpec {
	if in == nil {
		return nil
	}
	out := new(AgentSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AgentStatus) DeepCopyInto(out *AgentStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AgentStatus.
func (in *AgentStatus) DeepCopy() *AgentStatus {
	if in == nil {
		return nil
	}
	out := new(AgentStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AnyType) DeepCopyInto(out *AnyType) {
	*out = *in
	if in.RawMessage != nil {
		in, out := &in.RawMessage, &out.RawMessage
		*out = make(json.RawMessage, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AnyType.
func (in *AnyType) DeepCopy() *AnyType {
	if in == nil {
		return nil
	}
	out := new(AnyType)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BuiltinTool) DeepCopyInto(out *BuiltinTool) {
	*out = *in
	if in.Config != nil {
		in, out := &in.Config, &out.Config
		*out = make(map[string]AnyType, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BuiltinTool.
func (in *BuiltinTool) DeepCopy() *BuiltinTool {
	if in == nil {
		return nil
	}
	out := new(BuiltinTool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Component) DeepCopyInto(out *Component) {
	*out = *in
	if in.Config != nil {
		in, out := &in.Config, &out.Config
		*out = make(map[string]AnyType, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Component.
func (in *Component) DeepCopy() *Component {
	if in == nil {
		return nil
	}
	out := new(Component)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPTool) DeepCopyInto(out *MCPTool) {
	*out = *in
	in.Component.DeepCopyInto(&out.Component)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPTool.
func (in *MCPTool) DeepCopy() *MCPTool {
	if in == nil {
		return nil
	}
	out := new(MCPTool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPToolServerParams) DeepCopyInto(out *MCPToolServerParams) {
	*out = *in
	if in.Stdio != nil {
		in, out := &in.Stdio, &out.Stdio
		*out = new(StdioMcpServerConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Sse != nil {
		in, out := &in.Sse, &out.Sse
		*out = new(SseMcpServerConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPToolServerParams.
func (in *MCPToolServerParams) DeepCopy() *MCPToolServerParams {
	if in == nil {
		return nil
	}
	out := new(MCPToolServerParams)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MagenticOneTeamConfig) DeepCopyInto(out *MagenticOneTeamConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MagenticOneTeamConfig.
func (in *MagenticOneTeamConfig) DeepCopy() *MagenticOneTeamConfig {
	if in == nil {
		return nil
	}
	out := new(MagenticOneTeamConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MaxMessageTermination) DeepCopyInto(out *MaxMessageTermination) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MaxMessageTermination.
func (in *MaxMessageTermination) DeepCopy() *MaxMessageTermination {
	if in == nil {
		return nil
	}
	out := new(MaxMessageTermination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *McpServerTool) DeepCopyInto(out *McpServerTool) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new McpServerTool.
func (in *McpServerTool) DeepCopy() *McpServerTool {
	if in == nil {
		return nil
	}
	out := new(McpServerTool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ModelConfig) DeepCopyInto(out *ModelConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ModelConfig.
func (in *ModelConfig) DeepCopy() *ModelConfig {
	if in == nil {
		return nil
	}
	out := new(ModelConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ModelConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ModelConfigList) DeepCopyInto(out *ModelConfigList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ModelConfig, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ModelConfigList.
func (in *ModelConfigList) DeepCopy() *ModelConfigList {
	if in == nil {
		return nil
	}
	out := new(ModelConfigList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ModelConfigList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ModelConfigSpec) DeepCopyInto(out *ModelConfigSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ModelConfigSpec.
func (in *ModelConfigSpec) DeepCopy() *ModelConfigSpec {
	if in == nil {
		return nil
	}
	out := new(ModelConfigSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ModelConfigStatus) DeepCopyInto(out *ModelConfigStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ModelConfigStatus.
func (in *ModelConfigStatus) DeepCopy() *ModelConfigStatus {
	if in == nil {
		return nil
	}
	out := new(ModelConfigStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OrTermination) DeepCopyInto(out *OrTermination) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]OrTerminationCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OrTermination.
func (in *OrTermination) DeepCopy() *OrTermination {
	if in == nil {
		return nil
	}
	out := new(OrTermination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OrTerminationCondition) DeepCopyInto(out *OrTerminationCondition) {
	*out = *in
	if in.MaxMessageTermination != nil {
		in, out := &in.MaxMessageTermination, &out.MaxMessageTermination
		*out = new(MaxMessageTermination)
		**out = **in
	}
	if in.TextMentionTermination != nil {
		in, out := &in.TextMentionTermination, &out.TextMentionTermination
		*out = new(TextMentionTermination)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OrTerminationCondition.
func (in *OrTerminationCondition) DeepCopy() *OrTerminationCondition {
	if in == nil {
		return nil
	}
	out := new(OrTerminationCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RoundRobinTeamConfig) DeepCopyInto(out *RoundRobinTeamConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RoundRobinTeamConfig.
func (in *RoundRobinTeamConfig) DeepCopy() *RoundRobinTeamConfig {
	if in == nil {
		return nil
	}
	out := new(RoundRobinTeamConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SelectorTeamConfig) DeepCopyInto(out *SelectorTeamConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SelectorTeamConfig.
func (in *SelectorTeamConfig) DeepCopy() *SelectorTeamConfig {
	if in == nil {
		return nil
	}
	out := new(SelectorTeamConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SseMcpServerConfig) DeepCopyInto(out *SseMcpServerConfig) {
	*out = *in
	if in.Headers != nil {
		in, out := &in.Headers, &out.Headers
		*out = make(map[string]AnyType, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SseMcpServerConfig.
func (in *SseMcpServerConfig) DeepCopy() *SseMcpServerConfig {
	if in == nil {
		return nil
	}
	out := new(SseMcpServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *StdioMcpServerConfig) DeepCopyInto(out *StdioMcpServerConfig) {
	*out = *in
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Env != nil {
		in, out := &in.Env, &out.Env
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new StdioMcpServerConfig.
func (in *StdioMcpServerConfig) DeepCopy() *StdioMcpServerConfig {
	if in == nil {
		return nil
	}
	out := new(StdioMcpServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *StopMessageTermination) DeepCopyInto(out *StopMessageTermination) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new StopMessageTermination.
func (in *StopMessageTermination) DeepCopy() *StopMessageTermination {
	if in == nil {
		return nil
	}
	out := new(StopMessageTermination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SwarmTeamConfig) DeepCopyInto(out *SwarmTeamConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SwarmTeamConfig.
func (in *SwarmTeamConfig) DeepCopy() *SwarmTeamConfig {
	if in == nil {
		return nil
	}
	out := new(SwarmTeamConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Team) DeepCopyInto(out *Team) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Team.
func (in *Team) DeepCopy() *Team {
	if in == nil {
		return nil
	}
	out := new(Team)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Team) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TeamList) DeepCopyInto(out *TeamList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Team, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TeamList.
func (in *TeamList) DeepCopy() *TeamList {
	if in == nil {
		return nil
	}
	out := new(TeamList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TeamList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TeamSpec) DeepCopyInto(out *TeamSpec) {
	*out = *in
	if in.Participants != nil {
		in, out := &in.Participants, &out.Participants
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.RoundRobinTeamConfig != nil {
		in, out := &in.RoundRobinTeamConfig, &out.RoundRobinTeamConfig
		*out = new(RoundRobinTeamConfig)
		**out = **in
	}
	if in.SelectorTeamConfig != nil {
		in, out := &in.SelectorTeamConfig, &out.SelectorTeamConfig
		*out = new(SelectorTeamConfig)
		**out = **in
	}
	if in.MagenticOneTeamConfig != nil {
		in, out := &in.MagenticOneTeamConfig, &out.MagenticOneTeamConfig
		*out = new(MagenticOneTeamConfig)
		**out = **in
	}
	if in.SwarmTeamConfig != nil {
		in, out := &in.SwarmTeamConfig, &out.SwarmTeamConfig
		*out = new(SwarmTeamConfig)
		**out = **in
	}
	in.TerminationCondition.DeepCopyInto(&out.TerminationCondition)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TeamSpec.
func (in *TeamSpec) DeepCopy() *TeamSpec {
	if in == nil {
		return nil
	}
	out := new(TeamSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TeamStatus) DeepCopyInto(out *TeamStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TeamStatus.
func (in *TeamStatus) DeepCopy() *TeamStatus {
	if in == nil {
		return nil
	}
	out := new(TeamStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TerminationCondition) DeepCopyInto(out *TerminationCondition) {
	*out = *in
	if in.MaxMessageTermination != nil {
		in, out := &in.MaxMessageTermination, &out.MaxMessageTermination
		*out = new(MaxMessageTermination)
		**out = **in
	}
	if in.TextMentionTermination != nil {
		in, out := &in.TextMentionTermination, &out.TextMentionTermination
		*out = new(TextMentionTermination)
		**out = **in
	}
	if in.TextMessageTermination != nil {
		in, out := &in.TextMessageTermination, &out.TextMessageTermination
		*out = new(TextMessageTermination)
		**out = **in
	}
	if in.StopMessageTermination != nil {
		in, out := &in.StopMessageTermination, &out.StopMessageTermination
		*out = new(StopMessageTermination)
		**out = **in
	}
	if in.OrTermination != nil {
		in, out := &in.OrTermination, &out.OrTermination
		*out = new(OrTermination)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TerminationCondition.
func (in *TerminationCondition) DeepCopy() *TerminationCondition {
	if in == nil {
		return nil
	}
	out := new(TerminationCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TextMentionTermination) DeepCopyInto(out *TextMentionTermination) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TextMentionTermination.
func (in *TextMentionTermination) DeepCopy() *TextMentionTermination {
	if in == nil {
		return nil
	}
	out := new(TextMentionTermination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TextMessageTermination) DeepCopyInto(out *TextMessageTermination) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TextMessageTermination.
func (in *TextMessageTermination) DeepCopy() *TextMessageTermination {
	if in == nil {
		return nil
	}
	out := new(TextMessageTermination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Tool) DeepCopyInto(out *Tool) {
	*out = *in
	in.BuiltinTool.DeepCopyInto(&out.BuiltinTool)
	out.McpServerTool = in.McpServerTool
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Tool.
func (in *Tool) DeepCopy() *Tool {
	if in == nil {
		return nil
	}
	out := new(Tool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ToolServer) DeepCopyInto(out *ToolServer) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ToolServer.
func (in *ToolServer) DeepCopy() *ToolServer {
	if in == nil {
		return nil
	}
	out := new(ToolServer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ToolServer) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ToolServerConfig) DeepCopyInto(out *ToolServerConfig) {
	*out = *in
	if in.Stdio != nil {
		in, out := &in.Stdio, &out.Stdio
		*out = new(StdioMcpServerConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Sse != nil {
		in, out := &in.Sse, &out.Sse
		*out = new(SseMcpServerConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ToolServerConfig.
func (in *ToolServerConfig) DeepCopy() *ToolServerConfig {
	if in == nil {
		return nil
	}
	out := new(ToolServerConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ToolServerList) DeepCopyInto(out *ToolServerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ToolServer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ToolServerList.
func (in *ToolServerList) DeepCopy() *ToolServerList {
	if in == nil {
		return nil
	}
	out := new(ToolServerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ToolServerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ToolServerSpec) DeepCopyInto(out *ToolServerSpec) {
	*out = *in
	in.Config.DeepCopyInto(&out.Config)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ToolServerSpec.
func (in *ToolServerSpec) DeepCopy() *ToolServerSpec {
	if in == nil {
		return nil
	}
	out := new(ToolServerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ToolServerStatus) DeepCopyInto(out *ToolServerStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.DiscoveredTools != nil {
		in, out := &in.DiscoveredTools, &out.DiscoveredTools
		*out = make([]*MCPTool, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(MCPTool)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ToolServerStatus.
func (in *ToolServerStatus) DeepCopy() *ToolServerStatus {
	if in == nil {
		return nil
	}
	out := new(ToolServerStatus)
	in.DeepCopyInto(out)
	return out
}
