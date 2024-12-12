//go:build !ignore_autogenerated

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IBMNodeClass) DeepCopyInto(out *IBMNodeClass) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IBMNodeClass.
func (in *IBMNodeClass) DeepCopy() *IBMNodeClass {
	if in == nil {
		return nil
	}
	out := new(IBMNodeClass)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *IBMNodeClass) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IBMNodeClassList) DeepCopyInto(out *IBMNodeClassList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]IBMNodeClass, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IBMNodeClassList.
func (in *IBMNodeClassList) DeepCopy() *IBMNodeClassList {
	if in == nil {
		return nil
	}
	out := new(IBMNodeClassList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *IBMNodeClassList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IBMNodeClassSpec) DeepCopyInto(out *IBMNodeClassSpec) {
	*out = *in
	if in.InstanceRequirements != nil {
		in, out := &in.InstanceRequirements, &out.InstanceRequirements
		*out = new(InstanceTypeRequirements)
		**out = **in
	}
	if in.PlacementStrategy != nil {
		in, out := &in.PlacementStrategy, &out.PlacementStrategy
		*out = new(PlacementStrategy)
		(*in).DeepCopyInto(*out)
	}
	if in.SecurityGroups != nil {
		in, out := &in.SecurityGroups, &out.SecurityGroups
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IBMNodeClassSpec.
func (in *IBMNodeClassSpec) DeepCopy() *IBMNodeClassSpec {
	if in == nil {
		return nil
	}
	out := new(IBMNodeClassSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IBMNodeClassStatus) DeepCopyInto(out *IBMNodeClassStatus) {
	*out = *in
	in.LastValidationTime.DeepCopyInto(&out.LastValidationTime)
	if in.SelectedInstanceTypes != nil {
		in, out := &in.SelectedInstanceTypes, &out.SelectedInstanceTypes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.SelectedSubnets != nil {
		in, out := &in.SelectedSubnets, &out.SelectedSubnets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IBMNodeClassStatus.
func (in *IBMNodeClassStatus) DeepCopy() *IBMNodeClassStatus {
	if in == nil {
		return nil
	}
	out := new(IBMNodeClassStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *InstanceTypeRequirements) DeepCopyInto(out *InstanceTypeRequirements) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new InstanceTypeRequirements.
func (in *InstanceTypeRequirements) DeepCopy() *InstanceTypeRequirements {
	if in == nil {
		return nil
	}
	out := new(InstanceTypeRequirements)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PlacementStrategy) DeepCopyInto(out *PlacementStrategy) {
	*out = *in
	if in.SubnetSelection != nil {
		in, out := &in.SubnetSelection, &out.SubnetSelection
		*out = new(SubnetSelectionCriteria)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PlacementStrategy.
func (in *PlacementStrategy) DeepCopy() *PlacementStrategy {
	if in == nil {
		return nil
	}
	out := new(PlacementStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubnetSelectionCriteria) DeepCopyInto(out *SubnetSelectionCriteria) {
	*out = *in
	if in.RequiredTags != nil {
		in, out := &in.RequiredTags, &out.RequiredTags
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubnetSelectionCriteria.
func (in *SubnetSelectionCriteria) DeepCopy() *SubnetSelectionCriteria {
	if in == nil {
		return nil
	}
	out := new(SubnetSelectionCriteria)
	in.DeepCopyInto(out)
	return out
}