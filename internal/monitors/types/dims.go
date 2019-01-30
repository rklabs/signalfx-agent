package types

// DimProperties represents a set of properties associated with a given
// dimension value
type DimProperties struct {
	Dimension
	// Properties to be set on the dimension
	Properties map[string]string
	// Tags to apply to the dimension value
	Tags map[string]bool
}

// Dimension represents a specific dimension value
type Dimension struct {
	// Name of the dimension
	Name string
	// Value of the dimension
	Value string
}

func (dp *DimProperties) Copy() *DimProperties {
	clonedDimension := Dimension{
		Name:  dp.Dimension.Name,
		Value: dp.Dimension.Value,
	}

	clonedProperties := make(map[string]string)
	for k, v := range dp.Properties {
		clonedProperties[k] = v
	}

	clonedTags := make(map[string]bool)
	for k, v := range dp.Tags {
		clonedTags[k] = v
	}

	return &DimProperties{
		Dimension:  clonedDimension,
		Properties: clonedProperties,
		Tags:       clonedTags,
	}
}
