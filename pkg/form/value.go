package form

import (
	"errors"
	"strconv"
	"strings"
)

type ElementValueType interface {
	string | int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64 | float32 | float64 | bool
}

func parseValue[T ElementValueType](formValue string, dstVal *T, dstErr *string) {
	switch v := any(dstVal).(type) {
	case *string:
		*v = formValue
	case *int:
		if val, err := strconv.Atoi(formValue); err == nil {
			*v = val
		} else {
			*dstErr = numError(err)
		}
	case *int8:
		if val, err := strconv.ParseInt(formValue, 10, 8); err == nil {
			*v = int8(val)
		} else {
			*dstErr = numError(err)
		}
	case *int16:
		if val, err := strconv.ParseInt(formValue, 10, 16); err == nil {
			*v = int16(val)
		} else {
			*dstErr = numError(err)
		}
	case *int32:
		if val, err := strconv.ParseInt(formValue, 10, 32); err == nil {
			*v = int32(val)
		} else {
			*dstErr = numError(err)
		}
	case *int64:
		if val, err := strconv.ParseInt(formValue, 10, 64); err == nil {
			*v = val
		} else {
			*dstErr = numError(err)
		}
	case *uint:
		if val, err := strconv.ParseUint(formValue, 10, 64); err == nil {
			*v = uint(val)
		} else {
			*dstErr = numError(err)
		}
	case *uint8:
		if val, err := strconv.ParseUint(formValue, 10, 8); err == nil {
			*v = uint8(val)
		} else {
			*dstErr = numError(err)
		}
	case *uint16:
		if val, err := strconv.ParseUint(formValue, 10, 16); err == nil {
			*v = uint16(val)
		} else {
			*dstErr = numError(err)
		}
	case *uint32:
		if val, err := strconv.ParseUint(formValue, 10, 32); err == nil {
			*v = uint32(val)
		} else {
			*dstErr = numError(err)
		}
	case *uint64:
		if val, err := strconv.ParseUint(formValue, 10, 64); err == nil {
			*v = val
		} else {
			*dstErr = numError(err)
		}
	case *float32:
		if val, err := strconv.ParseFloat(formValue, 32); err == nil {
			*v = float32(val)
		} else {
			*dstErr = numError(err)
		}
	case *float64:
		if val, err := strconv.ParseFloat(formValue, 64); err == nil {
			*v = val
		} else {
			*dstErr = numError(err)
		}
	case *bool:
		if val, err := strconv.ParseBool(formValue); err == nil {
			*v = val
		} else {
			*dstErr = numError(err)
		}
	default:
		// Should never happen
		panic("Unsupported type")
	}
}

func ucFirst(s string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// numError returns the reason a strconv parse failed, capitalised.
func numError(err error) string {
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		err = ne.Err
	}
	return ucFirst(err.Error())
}
