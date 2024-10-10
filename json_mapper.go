package jsonmapper

import (
	"encoding/json"
	"errors"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"io"
	"os"
	"sync"
	"time"
)

var jsonIter = jsoniter.ConfigCompatibleWithStandardLibrary

// JsonMapper represents a generic JSON type. It contains fields for all supported JSON
// types like bool, int, float, string, object, and array, as well as Go supported types.
type JsonMapper struct {
	IsBool   bool
	IsInt    bool
	IsFloat  bool
	IsString bool
	IsObject bool
	IsArray  bool
	IsNull   bool

	AsBool   bool
	AsInt    int
	AsFloat  float64
	AsString string
	AsObject JsonObject
	AsArray  JsonArray

	buffer   []byte
	offset   int
	lastRead int
	reader   io.Reader
}

// FromBytes parses JSON data from a byte slice.
func FromBytes(data []byte) (JsonMapper, error) {
	if isObjectOrArray(data, '[') {
		return newJsonArray(data)
	} else if isObjectOrArray(data, '{') {
		return newJsonObject(data)
	} else {
		return JsonMapper{}, errors.New("could not parse JSON")
	}
}

// FromStruct serializes a Go struct into JSON and parses it into a JsonMapper object.
func FromStruct[T any](s T) (JsonMapper, error) {
	jsonBytes, err := marshal(s)
	if err != nil {
		return JsonMapper{}, err
	}
	return FromBytes(jsonBytes)
}

// FromFile reads a JSON file from the given path and parses it into a JsonMapper object.
func FromFile(path string) (JsonMapper, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return JsonMapper{}, err
	}
	return FromBytes(file)
}

// FromString parses JSON from a string into a JsonMapper object.
func FromString(data string) (JsonMapper, error) {
	return FromBytes([]byte(data))
}

func FromBuffer(reader io.Reader) (JsonMapper, error) {
	var m JsonMapper
	m.reader = reader
	m.buffer = make([]byte, 4096)
	return m, nil
}

// AsTime attempts to convert the JSON value to a time.Time object.
// Only works if the JSON value is a string and can be parsed as a valid time.
func (m *JsonMapper) AsTime() (time.Time, error) {
	if !m.IsString {
		return time.Time{}, NewTimeTypeConversionErr(m.getType())
	}
	for _, layout := range timeLayouts {
		parsedTime, err := time.Parse(layout, m.AsString)
		if err == nil {
			return parsedTime, nil
		}
	}
	return time.Time{}, NewInvalidTimeErr(m.AsString)
}

func (m *JsonMapper) ProcessJsonList(numberOfWorkers int, f func(o JsonObject)) {
	dec := json.NewDecoder(m.reader)
	_, err := dec.Token()
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, numberOfWorkers)

	for dec.More() {
		var data map[string]*interface{}
		err = dec.Decode(&data)
		if err == io.EOF {
			break
		} else if err != nil {
			return
		}
		obj := NewObject(data)
		wg.Add(1)
		sem <- struct{}{}
		go func(o JsonObject) {
			defer func() { <-sem }()
			f(o)
			wg.Done()
		}(*obj)
	}

	_, err = dec.Token()
	if err != nil {
		return
	}
	wg.Wait()
}

func (m *JsonMapper) Read(p []byte) (n int, err error) {
	m.lastRead = 0
	if len(m.buffer) <= m.offset {
		// Buffer is empty, reset to recover space.
		m.reset()
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	n = copy(p, m.buffer[m.offset:])
	m.offset += n
	if n > 0 {
		m.lastRead = -1
	}
	return n, nil
}

// PrettyString returns a formatted, human-readable string representation of the JsonMapper value.
func (m *JsonMapper) PrettyString() string {
	if m.IsBool {
		return fmt.Sprintf("%v", m.AsBool)
	} else if m.IsInt {
		return fmt.Sprintf("%v", m.AsInt)
	} else if m.IsFloat {
		return fmt.Sprintf("%v", m.AsFloat)
	} else if m.IsString {
		return fmt.Sprintf("%v", m.AsString)
	} else if m.IsObject {
		return m.AsObject.PrettyString()
	} else if m.IsArray {
		return fmt.Sprintf("%v", m.AsArray)
	}
	return ""
}

// String returns a string representation JsonMapper type in JSON format.
func (m *JsonMapper) String() string {
	switch {
	case m.IsBool:
		return fmt.Sprintf("%v", m.AsBool)
	case m.IsInt:
		return fmt.Sprintf("%v", m.AsInt)
	case m.IsFloat:
		return fmt.Sprintf("%v", m.AsFloat)
	case m.IsString:
		return fmt.Sprintf("%v", m.AsString)
	case m.IsObject:
		return fmt.Sprintf("%v", m.AsObject)
	case m.IsArray:
		return fmt.Sprintf("%v", m.AsArray)
	}
	return ""
}

func (m *JsonMapper) reset() {
	m.buffer = m.buffer[:0]
	m.offset = 0
	m.lastRead = 0
}
