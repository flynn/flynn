package grohl

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// BuildLog assembles a log message from the key/value data.  If addTime is true,
// the current timestamp is logged with the "now" key.
func BuildLog(data Data, addTime bool) string {
	index := 0
	extraRows := 0
	if addTime {
		extraRows = extraRows + 1
		delete(data, "now")
	}

	pieces := make([]string, len(data)+extraRows)
	for key, value := range data {
		pieces[index+extraRows] = fmt.Sprintf("%s=%s", key, Format(value))
		index = index + 1
	}

	if addTime {
		pieces[0] = fmt.Sprintf("now=%s", time.Now().UTC().Format(timeLayout))
	}

	return strings.Join(pieces, space)
}

// Format converts the value into a string for the Logger output.
func Format(value interface{}) string {
	if value == nil {
		return "nil"
	}

	t := reflect.TypeOf(value)
	formatter := formatters[t.Kind().String()]
	if formatter == nil {
		formatter = formatters[t.String()]
	}

	if formatter == nil {
		if _, ok := t.MethodByName("Error"); ok == true {
			return formatString(value.(error).Error())
		} else {
			return formatString(fmt.Sprintf("%+v", value))
		}
	}

	return formatter(value)
}

func formatString(value interface{}) string {
	str := value.(string)

	if len(str) == 0 {
		return "nil"
	}

	if idx := strings.Index(str, " "); idx != -1 {
		hasSingle := strings.Index(str, sQuote) != -1
		hasDouble := strings.Index(str, dQuote) != -1
		str = strings.Replace(str, back, backReplace, -1)

		if hasSingle && hasDouble {
			str = dQuote + strings.Replace(str, dQuote, dReplace, -1) + dQuote
		} else if hasDouble {
			str = sQuote + str + sQuote
		} else {
			str = dQuote + str + dQuote
		}
	} else {
		if idx := strings.Index(str, "="); idx != -1 {
			str = dQuote + str + dQuote
		}
	}

	return str
}

const (
	space       = " "
	equals      = "="
	sQuote      = "'"
	dQuote      = `"`
	dReplace    = `\"`
	back        = `\`
	backReplace = `\\`
	timeLayout  = "2006-01-02T15:04:05-0700"
)

var durationFormat = []byte("f")[0]

var formatters = map[string]func(value interface{}) string{
	"string": formatString,

	"bool": func(value interface{}) string {
		return strconv.FormatBool(value.(bool))
	},

	"int": func(value interface{}) string {
		return strconv.FormatInt(int64(value.(int)), 10)
	},

	"int8": func(value interface{}) string {
		return strconv.FormatInt(int64(value.(int8)), 10)
	},

	"int16": func(value interface{}) string {
		return strconv.FormatInt(int64(value.(int16)), 10)
	},

	"int32": func(value interface{}) string {
		return strconv.FormatInt(int64(value.(int32)), 10)
	},

	"int64": func(value interface{}) string {
		return strconv.FormatInt(value.(int64), 10)
	},

	"float32": func(value interface{}) string {
		return strconv.FormatFloat(float64(value.(float32)), durationFormat, 3, 32)
	},

	"float64": func(value interface{}) string {
		return strconv.FormatFloat(value.(float64), durationFormat, 3, 64)
	},

	"uint": func(value interface{}) string {
		return strconv.FormatUint(uint64(value.(uint)), 10)
	},

	"uint8": func(value interface{}) string {
		return strconv.FormatUint(uint64(value.(uint8)), 10)
	},

	"uint16": func(value interface{}) string {
		return strconv.FormatUint(uint64(value.(uint16)), 10)
	},

	"uint32": func(value interface{}) string {
		return strconv.FormatUint(uint64(value.(uint32)), 10)
	},

	"uint64": func(value interface{}) string {
		return strconv.FormatUint(value.(uint64), 10)
	},

	"time.Time": func(value interface{}) string {
		return value.(time.Time).Format(timeLayout)
	},
}
