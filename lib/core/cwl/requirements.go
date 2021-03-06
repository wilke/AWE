package cwl

import (
	"errors"
	"fmt"
	"reflect"
	//"github.com/MG-RAST/AWE/lib/logger"

	"github.com/davecgh/go-spew/spew"
	//"github.com/mitchellh/mapstructure"
)

type Requirement interface {
	GetClass() string
}

type DummyRequirement struct {
	BaseRequirement `bson:",inline" yaml:",inline" json:",inline" mapstructure:",squash"`
}

func NewRequirement(class string, obj interface{}) (r Requirement, schemata []CWLType_Type, err error) {

	if class == "" {
		err = fmt.Errorf("class name empty")
		return
	}

	switch class {
	case "DockerRequirement":
		r, err = NewDockerRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewDockerRequirement returns: %s", err.Error())
			return
		}
		return
	case "ShellCommandRequirement":
		r, err = NewShellCommandRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewShellCommandRequirement returns: %s", err.Error())
			return
		}
		return
	case "ResourceRequirement":
		r, err = NewResourceRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewResourceRequirement returns: %s", err.Error())
			return
		}
		return
	case "InlineJavascriptRequirement":
		r, err = NewInlineJavascriptRequirementFromInterface(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewInlineJavascriptRequirement returns: %s", err.Error())
			return
		}
		return
	case "EnvVarRequirement":
		r, err = NewEnvVarRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewEnvVarRequirement returns: %s", err.Error())
			return
		}
		return
	case "StepInputExpressionRequirement":
		r, err = NewStepInputExpressionRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewStepInputExpressionRequirement returns: %s", err.Error())
			return
		}
		return
	case "ShockRequirement":
		r, err = NewShockRequirementFromInterface(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewShockRequirementFromInterface returns: %s", err.Error())
			return
		}
		return
	case "InitialWorkDirRequirement":
		r, err = NewInitialWorkDirRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewInitialWorkDirRequirement returns: %s", err.Error())
			return
		}
		return
	case "ScatterFeatureRequirement":
		r, err = NewScatterFeatureRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewScatterFeatureRequirement returns: %s", err.Error())
			return
		}
		return
	case "MultipleInputFeatureRequirement":
		r, err = NewMultipleInputFeatureRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewMultipleInputFeatureRequirement returns: %s", err.Error())
			return
		}
		return
	case "SchemaDefRequirement":
		r, schemata, err = NewSchemaDefRequirement(obj)
		if err != nil {
			err = fmt.Errorf("(NewRequirement) NewSchemaDefRequirement returns: %s", err.Error())
			return
		}
		return

	case "SubworkflowFeatureRequirement":
		this_r := DummyRequirement{}
		this_r.Class = "SubworkflowFeatureRequirement"
		r = this_r
	default:
		err = errors.New("Requirement class not supported " + class)

	}
	return
}

func GetRequirement(r_name string, array_ptr *[]Requirement) (requirement *Requirement, err error) {

	if array_ptr == nil {
		err = fmt.Errorf("(GetRequirement) requirement array is empty, %s not found", r_name)
		return
	}

	for i, _ := range *array_ptr {

		if (*array_ptr)[i].GetClass() == r_name {
			requirement = &(*array_ptr)[i]
			return
		}
	}
	fmt.Println("Requirement array:")
	spew.Dump(array_ptr)
	err = fmt.Errorf("(GetRequirement) requirement %s not found", r_name)

	return
}

func GetShockRequirement(array_ptr *[]Requirement) (shock_requirement *ShockRequirement, err error) {
	var requirement_ptr *Requirement
	requirement_ptr, err = GetRequirement("ShockRequirement", array_ptr)
	if err != nil {
		return
	}

	requirement := *requirement_ptr

	var ok bool
	shock_requirement, ok = requirement.(*ShockRequirement)
	if !ok {
		err = fmt.Errorf("(GetShockRequirement) could not convert ShockRequirement (type: %s)", reflect.TypeOf(requirement))
		return
	}
	//shock_requirement = &shock_requirement_nptr

	return
}

func AddRequirement(new_r Requirement, old_array_ptr *[]Requirement) (new_array_ptr *[]Requirement, err error) {

	var new_array []Requirement

	new_r_class := new_r.GetClass()
	if old_array_ptr != nil {
		for i, _ := range *old_array_ptr {
			r := (*old_array_ptr)[i]
			if r.GetClass() == new_r_class {
				new_array_ptr = old_array_ptr
				return
			}
		}
		new_array = append(*old_array_ptr, new_r)
	} else {
		new_array = []Requirement{new_r}
	}

	new_array_ptr = &new_array

	return
}

func DeleteRequirement(requirement_class string, old_array_ptr *[]Requirement) (new_array_ptr *[]Requirement, err error) {

	// if old array is empty anyway, there is nothing to delete
	if old_array_ptr == nil {
		new_array_ptr = nil
		return
	}

	var new_array []Requirement

	for i, _ := range *old_array_ptr {
		r := (*old_array_ptr)[i]
		if r.GetClass() != requirement_class {
			new_array = append(new_array, r)
		}
	}

	new_array_ptr = &new_array

	return
}

func CreateRequirementArray(original interface{}) (new_array_ptr *[]Requirement, schemata []CWLType_Type, err error) {
	// here the keynames are actually class names

	original, err = MakeStringMap(original)
	if err != nil {
		return
	}

	if original == nil {
		err = fmt.Errorf("(CreateRequirementArray) original == nil")
	}

	new_array := []Requirement{}

	switch original.(type) {
	case map[string]interface{}:
		for class_str, v := range original.(map[string]interface{}) {

			var schemata_new []CWLType_Type
			var requirement Requirement
			requirement, schemata_new, err = NewRequirement(class_str, v)
			if err != nil {
				err = fmt.Errorf("(CreateRequirementArray) A NewRequirement returns: %s", err)
				return
			}
			for i, _ := range schemata_new {
				schemata = append(schemata, schemata_new[i])
			}

			new_array = append(new_array, requirement)
		}
	case []interface{}:
		original_array := original.([]interface{})

		for i, _ := range original_array {
			v := original_array[i]

			var class_str string
			class_str, err = GetClass(v)
			if err != nil {
				return
			}

			//class := CWLType_Type(class_str)
			var schemata_new []CWLType_Type
			var requirement Requirement
			requirement, schemata_new, err = NewRequirement(class_str, v)
			if err != nil {
				fmt.Println("CreateRequirementArray:")
				spew.Dump(original)
				fmt.Println("CreateRequirementArray done")
				err = fmt.Errorf("(CreateRequirementArray) B NewRequirement returns: %s (%s)", err, spew.Sdump(v))
				return
			}
			for i, _ := range schemata_new {
				schemata = append(schemata, schemata_new[i])
			}
			new_array = append(new_array, requirement)

		}

	default:
		err = fmt.Errorf("(CreateRequirementArray) type %s unknown", reflect.TypeOf(original))
	}

	new_array_ptr = &new_array

	return
}
