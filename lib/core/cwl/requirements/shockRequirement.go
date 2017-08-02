package requirements

import (
	"github.com/mitchellh/mapstructure"
)

type ShockRequirement struct {
	BaseRequirement `bson:",inline" yaml:",inline" json:",inline"`
	Host            string `yaml:"host" bson:"host" json:"host"`
}

func (s ShockRequirement) GetId() string { return "None" }

func NewShockRequirement(original interface{}) (r *ShockRequirement, err error) {
	var requirement ShockRequirement
	r = &requirement
	err = mapstructure.Decode(original, &requirement)

	requirement.Class = "ShockRequirement"

	return
}
