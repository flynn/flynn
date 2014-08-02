package grohl

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var exampleTime = time.Date(2000, 1, 2, 3, 4, 5, 6, time.UTC)
var exampleError = fmt.Errorf("error message")

type ExampleStruct struct {
	Value interface{}
}

var examples = map[string]Data{
	"fn=string test=hi": Data{
		"fn": "string", "test": "hi",
	},
	`fn=stringspace test="a b"`: Data{
		"fn": "stringspace", "test": "a b",
	},
	`fn=stringslasher test="slasher \\\\"`: Data{
		"fn": "stringslasher", "test": `slasher \\`,
	},
	`fn=stringeqspace test="x=4, y=10"`: Data{
		"fn": "stringeqspace", "test": "x=4, y=10",
	},
	`fn=stringeq test="x=4,y=10"`: Data{
		"fn": "stringeq", "test": "x=4,y=10",
	},
	`fn=stringspace test="hello world"`: Data{
		"fn": "stringspace", "test": "hello world",
	},
	`fn=stringbothquotes test="echo 'hello' \"world\""`: Data{
		"fn": "stringbothquotes", "test": `echo 'hello' "world"`,
	},
	`fn=stringsinglequotes test="a 'a'"`: Data{
		"fn": "stringsinglequotes", "test": `a 'a'`,
	},
	`fn=stringdoublequotes test='echo "hello"'`: Data{
		"fn": "stringdoublequotes", "test": `echo "hello"`,
	},
	`fn=stringbothquotesnospace test='a"`: Data{
		"fn": "stringbothquotesnospace", "test": `'a"`,
	},
	"fn=emptystring test=nil": Data{
		"fn": "emptystring", "test": "",
	},
	"fn=int test=1": Data{
		"fn": "int", "test": int(1),
	},
	"fn=int8 test=1": Data{
		"fn": "int8", "test": int8(1),
	},
	"fn=int16 test=1": Data{
		"fn": "int16", "test": int16(1),
	},
	"fn=int32 test=1": Data{
		"fn": "int32", "test": int32(1),
	},
	"fn=int64 test=1": Data{
		"fn": "int64", "test": int64(1),
	},
	"fn=uint test=1": Data{
		"fn": "uint", "test": uint(1),
	},
	"fn=uint8 test=1": Data{
		"fn": "uint8", "test": uint8(1),
	},
	"fn=uint16 test=1": Data{
		"fn": "uint16", "test": uint16(1),
	},
	"fn=uint32 test=1": Data{
		"fn": "uint32", "test": uint32(1),
	},
	"fn=uint64 test=1": Data{
		"fn": "uint64", "test": uint64(1),
	},
	"fn=float test=1.000": Data{
		"fn": "float", "test": float32(1.0),
	},
	"fn=bool test=true": Data{
		"fn": "bool", "test": true,
	},
	"fn=nil test=nil": Data{
		"fn": "nil", "test": nil,
	},
	"fn=time test=2000-01-02T03:04:05+0000": Data{
		"fn": "time", "test": exampleTime,
	},
	`fn=error test="error message"`: Data{
		"fn": "error", "test": exampleError,
	},
	`fn=slice test="[86 87 88]"`: Data{
		"fn": "slice", "test": []byte{86, 87, 88},
	},
	`fn=struct test={Value:testing123}`: Data{
		"fn": "struct", "test": ExampleStruct{Value: "testing123"},
	},
}

func TestFormat(t *testing.T) {
	for expected, data := range examples {
		if actual := BuildLog(data, false); expected != actual {
			t.Errorf("Expected %s\nGot: %s", expected, actual)
		}
	}
}

func TestFormatWithTime(t *testing.T) {
	data := Data{"fn": "time", "test": 1}
	actual := BuildLog(data, true)
	if !strings.HasPrefix(actual, "now=") {
		t.Errorf("Invalid prefix: %s", actual)
	}
	if !strings.HasSuffix(actual, " fn=time test=1") {
		t.Errorf("Invalid suffix: %s", actual)
	}
}
