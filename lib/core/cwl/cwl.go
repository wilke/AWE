package cwl

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MG-RAST/AWE/lib/logger"
	"github.com/davecgh/go-spew/spew"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v2"
	//"io/ioutil"
	//"os"
	_ "reflect"
	"strings"
)

// this is used by YAML or JSON library for inital parsing
type CWL_document_generic struct {
	CwlVersion string               `yaml:"cwlVersion"`
	Graph      []CWL_object_generic `yaml:"graph"`
}

type CWL_object interface {
	GetClass() string
	GetId() string
	SetId(string)
}

type CWL_object_generic map[string]interface{}

// CWLType
// httpCWL_objectmonwl.org/v1.0/CommandLineTool.html#CWLType
// null, boolean, int, long, float, double, string, File, Directory
type CWLType interface {
	is_CWLType()
}

type CWLVersion interface{} // TODO

// generic class to represent Files and Directories
type CWL_location interface {
	GetLocation() string
}

type Any interface {
	CWL_object
	String() string
}

type LinkMergeMethod string // merge_nested or merge_flattened

func Parse_cwl_document(collection *CWL_collection, yaml_str string) (err error) {

	// TODO check cwlVersion
	// TODO screen for "$import": // this might break the YAML parser !

	// this yaml parser (gopkg.in/yaml.v2) has problems with the CWL yaml format. We skip the header aand jump directly to "$graph" because of that.
	graph_pos := strings.Index(yaml_str, "$graph:")

	if graph_pos == -1 {
		err = errors.New("yaml parisng error. keyword $graph missing")
		return
	}

	yaml_str = strings.Replace(yaml_str, "$graph", "graph", -1) // remove dollar sign

	cwl_gen := CWL_document_generic{}

	err = Unmarshal([]byte(yaml_str), &cwl_gen)
	if err != nil {
		logger.Debug(1, "CWL unmarshal error")
		logger.Error("error: " + err.Error())
	}

	fmt.Println("-------------- raw CWL")
	spew.Dump(cwl_gen)
	fmt.Println("-------------- Start real parsing")

	// iterated over Graph
	for _, elem := range cwl_gen.Graph {

		cwl_object_type, ok := elem["class"].(string)

		if !ok {
			err = errors.New("object has no member class")
			return
		}

		cwl_object_id := elem["id"].(string)
		if !ok {
			err = errors.New("object has no member id")
			return
		}
		_ = cwl_object_id
		switch elem["hints"].(type) {
		case map[interface{}]interface{}:
			// Convert map of outputs into array of outputs
			err, elem["hints"] = CreateRequirementArray(elem["hints"])
			if err != nil {
				return
			}
		}

		switch cwl_object_type {
		case "CommandLineTool":

			result, xerr := getCommandLineTool(elem)
			if xerr != nil {
				err = xerr
				return
			}
			//*** check if "inputs"" is an array or a map"

			//collection.CommandLineTools[result.Id] = result
			err = collection.Add(result)
			if err != nil {
				return
			}
			//collection = append(collection, result)
		case "Workflow":

			workflow, xerr := getWorkflow(elem)
			if xerr != nil {
				err = xerr
				return
			}

			// some checks and add inputs to collection
			for _, input := range workflow.Inputs {
				// input is InputParameter

				if input.Id == "" {
					err = fmt.Errorf("input has no ID")
					return
				}
				if !strings.HasPrefix(input.Id, "inputs.") {
					input.Id = "inputs." + input.Id
				}
				err = collection.Add(input)
				if err != nil {
					return
				}
			}

			//fmt.Println("WORKFLOW")
			//spew.Dump(workflow)
			err = collection.Add(&workflow)
			if err != nil {
				return
			}

			//collection.Workflows = append(collection.Workflows, workflow)
			//collection = append(collection, result)
		case "File":
			var cwl_file File
			err = mapstructure.Decode(elem, &cwl_file)
			if err != nil {
				return
			}
			if cwl_file.Id == "" {
				cwl_file.Id = cwl_object_id
			}
			//collection.Files[cwl_file.Id] = cwl_file
			err = collection.Add(&cwl_file)
			if err != nil {
				return
			}
		default:
			err = errors.New("object unknown")
			return
		} // end switch

		fmt.Printf("----------------------------------------------\n")

	} // end for

	return
}

func Unmarshal(data []byte, v interface{}) (err error) {
	err_yaml := yaml.Unmarshal(data, v)
	if err_yaml != nil {
		logger.Debug(1, "CWL YAML unmarshal error, (try json...) : "+err_yaml.Error())
		err_json := json.Unmarshal(data, v)
		if err_json != nil {
			logger.Debug(1, "CWL JSON unmarshal error: "+err_json.Error())
		}
	}

	if err != nil {
		err = errors.New("Could not parse document as JSON or YAML")
	}

	return
}
