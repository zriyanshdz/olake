package types

import (
	"fmt"
	"sync"

	"github.com/datazip-inc/olake/utils"
	"github.com/fraugster/parquet-go/parquet"
	"github.com/fraugster/parquet-go/parquetschema"
	"github.com/goccy/go-json"
)

type TypeSchema struct {
	mu         sync.Mutex
	Properties sync.Map `json:"-"`
}

func NewTypeSchema() *TypeSchema {
	return &TypeSchema{
		mu:         sync.Mutex{},
		Properties: sync.Map{},
	}
}

func (t *TypeSchema) Override(fields map[string]*Property) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for key, value := range fields {
		stored, loaded := t.Properties.LoadOrStore(key, value)
		if loaded && stored.(*Property).Nullable() {
			value.Type.Insert(NULL)
		}
	}
}

// MarshalJSON custom marshaller to handle sync.Map encoding
func (t *TypeSchema) MarshalJSON() ([]byte, error) {
	// Create a map to temporarily store data for JSON marshalling
	propertiesMap := make(map[string]*Property)
	t.Properties.Range(func(key, value interface{}) bool {
		strKey, ok := key.(string)
		if !ok {
			return false
		}
		prop, ok := value.(*Property)
		if !ok {
			return false
		}
		propertiesMap[strKey] = prop
		return true
	})

	// Create an alias to avoid infinite recursion
	type Alias TypeSchema
	return json.Marshal(&struct {
		*Alias
		Properties map[string]*Property `json:"properties,omitempty"`
	}{
		Alias:      (*Alias)(t),
		Properties: propertiesMap,
	})
}

// UnmarshalJSON custom unmarshaller to handle sync.Map decoding
func (t *TypeSchema) UnmarshalJSON(data []byte) error {
	// Create a temporary structure to unmarshal JSON into
	type Alias TypeSchema
	aux := &struct {
		*Alias
		Properties map[string]*Property `json:"properties,omitempty"`
	}{
		Alias: (*Alias)(t),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Populate sync.Map with the data from temporary map
	for key, value := range aux.Properties {
		t.Properties.Store(key, value)
	}

	return nil
}

func (t *TypeSchema) GetType(column string) (DataType, error) {
	p, found := t.Properties.Load(column)
	if !found {
		return "", fmt.Errorf("column [%s] missing from type schema", column)
	}

	return p.(*Property).DataType(), nil
}

func (t *TypeSchema) AddTypes(column string, types ...DataType) {
	p, found := t.Properties.Load(column)
	if !found {
		t.Properties.Store(column, &Property{
			Type: NewSet(types...),
		})
		return
	}

	property := p.(*Property)
	property.Type.Insert(types...)
}

func (t *TypeSchema) GetProperty(column string) (bool, *Property) {
	p, found := t.Properties.Load(column)
	if !found {
		return false, nil
	}

	return true, p.(*Property)
}

func (t *TypeSchema) ToParquet() *parquetschema.SchemaDefinition {
	definition := parquetschema.SchemaDefinition{
		RootColumn: &parquetschema.ColumnDefinition{
			SchemaElement: &parquet.SchemaElement{},
		},
	}
	t.Properties.Range(func(key, value interface{}) bool {
		schemaElem := value.(*Property).DataType().ToParquet() // get parquet type
		schemaElem.Name = key.(string)                         // attach Column name
		definition.RootColumn.Children = append(definition.RootColumn.Children, &parquetschema.ColumnDefinition{
			SchemaElement: schemaElem,
		})
		return true
	})

	return &definition
}

// Property is a dto for catalog properties representation
type Property struct {
	Type *Set[DataType] `json:"type,omitempty"`
	// TODO: Decide to keep in the Protocol Or Not
	// Format string     `json:"format,omitempty"`
}

func (p *Property) DataType() DataType {
	types := p.Type.Array()
	i, found := utils.ArrayContains(types, func(elem DataType) bool {
		return elem != NULL
	})
	if !found {
		return NULL
	}

	return types[i]
}

func (p *Property) Nullable() bool {
	_, found := utils.ArrayContains(p.Type.Array(), func(elem DataType) bool {
		return elem == NULL
	})

	return found
}