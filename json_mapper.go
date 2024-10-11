package jsonmapper

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const bufferSize = 4096

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
	data     []byte
}

// FromBytes parses JSON data from a byte slice.
func FromBytes(data []byte) (JsonMapper, error) {
	if dataStartsWith(data, '[') {
		arrayBytes, err := NewArrayFromBytes(data)
		if err != nil {
			return JsonMapper{}, err
		}
		return JsonMapper{IsArray: true, AsArray: *arrayBytes}, nil
	}
	if dataStartsWith(data, '{') {
		objBytes, err := NewObjectFromBytes(data)
		if err != nil {
			return JsonMapper{}, err
		}
		return JsonMapper{IsObject: true, AsObject: *objBytes}, nil
	}
	asString := string(data)
	var mapper JsonMapper

	i, err := strconv.Atoi(asString)
	if err == nil {
		mapper.IsInt = true
		mapper.AsInt = i
		return JsonMapper{IsInt: true, AsInt: i}, nil
	}

	f, err := strconv.ParseFloat(asString, 64)
	if err == nil {
		mapper.IsFloat = true
		mapper.AsFloat = f
		return JsonMapper{IsFloat: true, AsFloat: f}, nil
	}

	b, err := strconv.ParseBool(asString)
	if err == nil {
		mapper.IsBool = true
		mapper.AsBool = b
		return JsonMapper{IsBool: true, AsBool: b}, nil
	}

	if asString == "null" {
		mapper.IsNull = true
		return JsonMapper{IsNull: true}, nil
	}

	asString = strings.Trim(asString, `"`)
	mapper.IsString = true
	mapper.AsString = asString
	return JsonMapper{IsString: true, AsString: asString}, nil
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
	m.buffer = make([]byte, bufferSize)
	return m, nil
}

// AsTime attempts to convert the JSON value to a time.Time object.
// Only works if the JSON value is a string and can be parsed as a valid time.
func (m *JsonMapper) AsTime() (time.Time, error) {
	if !m.IsString {
		return time.Time{}, TimeTypeConversionErr
	}
	for _, layout := range timeLayouts {
		parsedTime, err := time.Parse(layout, m.AsString)
		if err == nil {
			return parsedTime, nil
		}
	}
	return time.Time{}, createNewInvalidTimeErr(m.AsString)
}

func (m *JsonMapper) AsUUID() (uuid.UUID, error) {
	if !m.IsString {
		return uuid.Nil, nil
	}
	return uuid.Parse(m.AsString)
}

func (m *JsonMapper) ProcessObjectsWithArgs(numberOfWorkers int, f func(o JsonObject, args ...any), args ...any) error {
	if m.reader == nil {
		return errors.New("reader is not set")
	}
	if m.buffer == nil {
		m.buffer = make([]byte, bufferSize)
	}

	dec := json.NewDecoder(m.reader)
	_, err := dec.Token()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, numberOfWorkers)
	for dec.More() {
		var data map[string]*interface{}
		err = dec.Decode(&data)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		obj := newObject(data)
		wg.Add(1)
		sem <- struct{}{}
		go func(o JsonObject) {
			defer func() { <-sem }()
			f(o, args...)
			wg.Done()
		}(*obj)
	}

	_, err = dec.Token()
	if err != nil {
		return err
	}

	wg.Wait()
	return nil
}

func (m *JsonMapper) ProcessObjects(numberOfWorkers int, f func(o JsonObject)) error {
	return m.ProcessObjectsWithArgs(numberOfWorkers, func(o JsonObject, args ...any) {
		f(o)
	})
}

func (m *JsonMapper) Read(p []byte) (n int, err error) {
	m.lastRead = 0
	if len(m.buffer) <= m.offset {
		// Buffer is empty, resetBuffer to recover space.
		m.resetBuffer()
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

func getMapperFromField(data *any) JsonMapper {
	if data == nil {
		return JsonMapper{IsNull: true}
	}

	var mapper JsonMapper
	switch value := (*data).(type) {
	case bool:
		mapper.IsBool = true
		mapper.AsBool = value
	case int:
		mapper.IsInt = true
		mapper.AsInt = value
	case float64:
		if value == float64(int(value)) {
			mapper.IsInt = true
			mapper.AsInt = int(value)
		} else {
			mapper.IsFloat = true
		}
		mapper.AsFloat = value
	case string:
		mapper.IsString = true
		mapper.AsString = value
	case map[string]any:
		mapper.IsObject = true
		mapper.AsObject = convertAnyToObject(data, nil)
	case []float64:
		mapper.IsArray = true
		mapper.AsArray = convertSliceToJsonArray(value)
	case []int:
		mapper.IsArray = true
		mapper.AsArray = convertSliceToJsonArray(value)
	case []string:
		mapper.IsArray = true
		mapper.AsArray = convertSliceToJsonArray(value)
	case []bool:
		mapper.IsArray = true
		mapper.AsArray = convertSliceToJsonArray(value)
	case []*any:
		mapper.IsArray = true
		mapper.AsArray = *newArray(value)
	case []any:
		mapper.IsArray = true
		mapper.AsArray = *newArray(convertToSlicePtr(value))
	case nil:
		mapper.IsNull = true
	}
	return mapper
}

func (m *JsonMapper) resetBuffer() {
	m.buffer = m.buffer[:0]
	m.offset = 0
	m.lastRead = 0
}
