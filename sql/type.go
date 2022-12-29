// Copyright 2020-2021 Dolthub, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sql

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/dolthub/go-mysql-server/sql/types"
	"github.com/dolthub/vitess/go/sqltypes"
	"github.com/dolthub/vitess/go/vt/proto/query"
	"github.com/dolthub/vitess/go/vt/sqlparser"
	"github.com/shopspring/decimal"
	"gopkg.in/src-d/go-errors.v1"
)

var (
	// ErrNotTuple is returned when the value is not a tuple.
	ErrNotTuple = errors.NewKind("value of type %T is not a tuple")

	// ErrInvalidColumnNumber is returned when a tuple has an invalid number of
	// arguments.
	ErrInvalidColumnNumber = errors.NewKind("tuple should contain %d column(s), but has %d")

	ErrInvalidBaseType = errors.NewKind("%v is not a valid %v base type")

	// ErrConvertToSQL is returned when Convert failed.
	// It makes an error less verbose comparing to what spf13/cast returns.
	ErrConvertToSQL = errors.NewKind("incompatible conversion to SQL type: %s")
)

const (
	// DateLayout is the layout of the MySQL date format in the representation
	// Go understands.
	DateLayout = "2006-01-02"

	// TimestampDatetimeLayout is the formatting string with the layout of the timestamp
	// using the format of Go "time" package.
	TimestampDatetimeLayout = "2006-01-02 15:04:05.999999"
)

// Type represents a SQL type.
type Type interface {
	// Compare returns an integer comparing two values.
	// The result will be 0 if a==b, -1 if a < b, and +1 if a > b.
	Compare(interface{}, interface{}) (int, error)
	// Convert a value of a compatible type to a most accurate type.
	Convert(interface{}) (interface{}, error)
	// Equals returns whether the given type is equivalent to the calling type. All parameters are included in the
	// comparison, so ENUM("a", "b") is not equivalent to ENUM("a", "b", "c").
	Equals(otherType Type) bool
	// MaxTextResponseByteLength returns the maximum number of bytes needed to serialize an instance of this type as a string in a response over the wire for MySQL's text protocol – in other words, this is the maximum bytes needed to serialize any value of this type as human-readable text, NOT in a more compact, binary representation.
	MaxTextResponseByteLength() uint32
	// Promote will promote the current type to the largest representing type of the same kind, such as Int8 to Int64.
	Promote() Type
	// SQL returns the sqltypes.Value for the given value.
	// Implementations can optionally use |dest| to append
	// serialized data, but should not mutate existing data.
	SQL(ctx *Context, dest []byte, v interface{}) (sqltypes.Value, error)
	// Type returns the query.Type for the given Type.
	Type() query.Type
	// ValueType returns the Go type of the value returned by Convert().
	ValueType() reflect.Type
	// Zero returns the golang zero value for this type
	Zero() interface{}
	fmt.Stringer
}

type Type2 interface {
	Type

	// Compare2 returns an integer comparing two Values.
	Compare2(Value, Value) (int, error)
	// Convert2 converts a value of a compatible type.
	Convert2(Value) (Value, error)
	// Zero2 returns the zero Value for this type.
	Zero2() Value
	// SQL2 returns the sqltypes.Value for the given value
	SQL2(Value) (sqltypes.Value, error)
}

// SpatialColumnType is a node that contains a reference to all spatial types.
type SpatialColumnType interface {
	// GetSpatialTypeSRID returns the SRID value for spatial types.
	GetSpatialTypeSRID() (uint32, bool)
	// SetSRID sets SRID value for spatial types.
	SetSRID(uint32) Type
	// MatchSRID returns nil if column type SRID matches given value SRID otherwise returns error.
	MatchSRID(interface{}) error
}

// SystemVariableType represents a SQL type specifically (and only) used in system variables. Assigning any non-system
// variables a SystemVariableType will cause errors.
type SystemVariableType interface {
	Type
	// EncodeValue returns the given value as a string for storage.
	EncodeValue(interface{}) (string, error)
	// DecodeValue returns the original value given to EncodeValue from the given string. This is different from `Convert`,
	// as the encoded value may technically be an "illegal" value according to the type rules.
	DecodeValue(string) (interface{}, error)
}

// ApproximateTypeFromValue returns the closest matching type to the given value. For example, an int16 will return SMALLINT.
func ApproximateTypeFromValue(val interface{}) Type {
	switch v := val.(type) {
	case bool:
		return Boolean
	case int:
		if strconv.IntSize == 32 {
			return Int32
		}
		return Int64
	case int64:
		return Int64
	case int32:
		return Int32
	case int16:
		return Int16
	case int8:
		return Int8
	case uint:
		if strconv.IntSize == 32 {
			return Uint32
		}
		return Uint64
	case uint64:
		return Uint64
	case uint32:
		return Uint32
	case uint16:
		return Uint16
	case uint8:
		return Uint8
	case Timespan, time.Duration:
		return Time
	case time.Time:
		return types.Datetime
	case float32:
		return Float32
	case float64:
		return Float64
	case string:
		typ, err := CreateString(sqltypes.VarChar, int64(len(v)), Collation_Default)
		if err != nil {
			typ, err = CreateString(sqltypes.Text, int64(len(v)), Collation_Default)
			if err != nil {
				typ = LongText
			}
		}
		return typ
	case []byte:
		typ, err := CreateBinary(sqltypes.VarBinary, int64(len(v)))
		if err != nil {
			typ, err = CreateBinary(sqltypes.Blob, int64(len(v)))
			if err != nil {
				typ = LongBlob
			}
		}
		return typ
	case decimal.Decimal:
		str := v.String()
		dotIdx := strings.Index(str, ".")
		if len(str) > 66 {
			return Float64
		} else if dotIdx == -1 {
			typ, err := CreateDecimalType(uint8(len(str)), 0)
			if err != nil {
				return Float64
			}
			return typ
		} else {
			precision := uint8(len(str) - 1)
			scale := uint8(len(str) - dotIdx - 1)
			typ, err := CreateDecimalType(precision, scale)
			if err != nil {
				return Float64
			}
			return typ
		}
	case decimal.NullDecimal:
		if !v.Valid {
			return Float64
		}
		return ApproximateTypeFromValue(v.Decimal)
	case nil:
		return Null
	default:
		return LongText
	}
}

// ColumnTypeToType gets the column type using the column definition.
func ColumnTypeToType(ct *sqlparser.ColumnType) (Type, error) {
	switch strings.ToLower(ct.Type) {
	case "boolean", "bool":
		return Int8, nil
	case "tinyint":
		if ct.Unsigned {
			return Uint8, nil
		}
		return Int8, nil
	case "smallint":
		if ct.Unsigned {
			return Uint16, nil
		}
		return Int16, nil
	case "mediumint":
		if ct.Unsigned {
			return Uint24, nil
		}
		return Int24, nil
	case "int", "integer":
		if ct.Unsigned {
			return Uint32, nil
		}
		return Int32, nil
	case "bigint":
		if ct.Unsigned {
			return Uint64, nil
		}
		return Int64, nil
	case "float":
		if ct.Length != nil {
			precision, err := strconv.ParseInt(string(ct.Length.Val), 10, 8)
			if err != nil {
				return nil, err
			}
			if precision > 53 || precision < 0 {
				return nil, ErrInvalidColTypeDefinition.New(ct.String(), "Valid range for precision is 0-24 or 25-53")
			} else if precision > 24 {
				return Float64, nil
			} else {
				return Float32, nil
			}
		}
		return Float32, nil
	case "double", "real", "double precision":
		return Float64, nil
	case "decimal", "fixed", "dec", "numeric":
		precision := int64(0)
		scale := int64(0)
		if ct.Length != nil {
			var err error
			precision, err = strconv.ParseInt(string(ct.Length.Val), 10, 8)
			if err != nil {
				return nil, err
			}
		}
		if ct.Scale != nil {
			var err error
			scale, err = strconv.ParseInt(string(ct.Scale.Val), 10, 8)
			if err != nil {
				return nil, err
			}
		}
		return CreateColumnDecimalType(uint8(precision), uint8(scale))
	case "bit":
		length := int64(1)
		if ct.Length != nil {
			var err error
			length, err = strconv.ParseInt(string(ct.Length.Val), 10, 8)
			if err != nil {
				return nil, err
			}
		}
		return CreateBitType(uint8(length))
	case "tinyblob":
		return TinyBlob, nil
	case "blob":
		if ct.Length == nil {
			return Blob, nil
		}
		length, err := strconv.ParseInt(string(ct.Length.Val), 10, 64)
		if err != nil {
			return nil, err
		}
		return CreateBinary(sqltypes.Blob, length)
	case "mediumblob":
		return MediumBlob, nil
	case "longblob":
		return LongBlob, nil
	case "tinytext":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		return CreateString(sqltypes.Text, tinyTextBlobMax/collation.CharacterSet().MaxLength(), collation)
	case "text":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		if ct.Length == nil {
			return CreateString(sqltypes.Text, textBlobMax/collation.CharacterSet().MaxLength(), collation)
		}
		length, err := strconv.ParseInt(string(ct.Length.Val), 10, 64)
		if err != nil {
			return nil, err
		}
		return CreateString(sqltypes.Text, length, collation)
	case "mediumtext", "long", "long varchar":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		return CreateString(sqltypes.Text, mediumTextBlobMax/collation.CharacterSet().MaxLength(), collation)
	case "longtext":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		return CreateString(sqltypes.Text, longTextBlobMax/collation.CharacterSet().MaxLength(), collation)
	case "char", "character":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		length := int64(1)
		if ct.Length != nil {
			var err error
			length, err = strconv.ParseInt(string(ct.Length.Val), 10, 64)
			if err != nil {
				return nil, err
			}
		}
		return CreateString(sqltypes.Char, length, collation)
	case "nchar", "national char", "national character":
		length := int64(1)
		if ct.Length != nil {
			var err error
			length, err = strconv.ParseInt(string(ct.Length.Val), 10, 64)
			if err != nil {
				return nil, err
			}
		}
		return CreateString(sqltypes.Char, length, Collation_utf8mb3_general_ci)
	case "varchar", "character varying":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		if ct.Length == nil {
			return nil, fmt.Errorf("VARCHAR requires a length")
		}

		var strLen = string(ct.Length.Val)
		var length int64
		if strings.ToLower(strLen) == "max" {
			length = 16383
		} else {
			length, err = strconv.ParseInt(strLen, 10, 64)
			if err != nil {
				return nil, err
			}
		}
		return CreateString(sqltypes.VarChar, length, collation)
	case "nvarchar", "national varchar", "national character varying":
		if ct.Length == nil {
			return nil, fmt.Errorf("VARCHAR requires a length")
		}
		length, err := strconv.ParseInt(string(ct.Length.Val), 10, 64)
		if err != nil {
			return nil, err
		}
		return CreateString(sqltypes.VarChar, length, Collation_utf8mb3_general_ci)
	case "binary":
		length := int64(1)
		if ct.Length != nil {
			var err error
			length, err = strconv.ParseInt(string(ct.Length.Val), 10, 64)
			if err != nil {
				return nil, err
			}
		}
		return CreateString(sqltypes.Binary, length, Collation_binary)
	case "varbinary":
		if ct.Length == nil {
			return nil, fmt.Errorf("VARBINARY requires a length")
		}
		length, err := strconv.ParseInt(string(ct.Length.Val), 10, 64)
		if err != nil {
			return nil, err
		}
		return CreateString(sqltypes.VarBinary, length, Collation_binary)
	case "year":
		return Year, nil
	case "date":
		return types.Date, nil
	case "time":
		if ct.Length != nil {
			length, err := strconv.ParseInt(string(ct.Length.Val), 10, 64)
			if err != nil {
				return nil, err
			}
			switch length {
			case 0, 1, 2, 3, 4, 5:
				return nil, fmt.Errorf("TIME length not yet supported")
			case 6:
				return Time, nil
			default:
				return nil, fmt.Errorf("TIME only supports a length from 0 to 6")
			}
		}
		return Time, nil
	case "timestamp":
		return types.Timestamp, nil
	case "datetime":
		return types.Datetime, nil
	case "enum":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		return CreateEnumType(ct.EnumValues, collation)
	case "set":
		collation, err := ParseCollation(&ct.Charset, &ct.Collate, ct.BinaryCollate)
		if err != nil {
			return nil, err
		}
		return CreateSetType(ct.EnumValues, collation)
	case "json":
		return JSON, nil
	case "geometry":
		return GeometryType{}, nil
	case "geometrycollection":
		return GeomCollType{}, nil
	case "linestring":
		return LineStringType{}, nil
	case "multilinestring":
		return MultiLineStringType{}, nil
	case "point":
		return PointType{}, nil
	case "multipoint":
		return MultiPointType{}, nil
	case "polygon":
		return PolygonType{}, nil
	case "multipolygon":
		return MultiPolygonType{}, nil
	default:
		return nil, fmt.Errorf("unknown type: %v", ct.Type)
	}
	return nil, fmt.Errorf("type not yet implemented: %v", ct.Type)
}

func ConvertToBool(v interface{}) (bool, error) {
	switch b := v.(type) {
	case bool:
		if b {
			return true, nil
		}
		return false, nil
	case int:
		return ConvertToBool(int64(b))
	case int64:
		if b == 0 {
			return false, nil
		}
		return true, nil
	case int32:
		return ConvertToBool(int64(b))
	case int16:
		return ConvertToBool(int64(b))
	case int8:
		return ConvertToBool(int64(b))
	case uint:
		return ConvertToBool(int64(b))
	case uint64:
		if b == 0 {
			return false, nil
		}
		return true, nil
	case uint32:
		return ConvertToBool(uint64(b))
	case uint16:
		return ConvertToBool(uint64(b))
	case uint8:
		return ConvertToBool(uint64(b))
	case time.Duration:
		if b == 0 {
			return false, nil
		}
		return true, nil
	case time.Time:
		if b.UnixNano() == 0 {
			return false, nil
		}
		return true, nil
	case float32:
		return ConvertToBool(float64(b))
	case float64:
		if b == 0 {
			return false, nil
		}
		return true, nil
	case string:
		bFloat, err := strconv.ParseFloat(b, 64)
		if err != nil {
			// In MySQL, if the string does not represent a float then it's false
			return false, nil
		}
		return bFloat != 0, nil
	case nil:
		return false, fmt.Errorf("unable to cast nil to bool")
	default:
		return false, fmt.Errorf("unable to cast %#v of type %T to bool", v, v)
	}
}

// NumColumns returns the number of columns in a type. This is one for all
// types, except tuples.
func NumColumns(t Type) int {
	v, ok := t.(TupleType)
	if !ok {
		return 1
	}
	return len(v)
}

// ErrIfMismatchedColumns returns an operand error if the number of columns in
// t1 is not equal to the number of columns in t2. If the number of columns is
// equal, and both types are tuple types, it recurses into each subtype,
// asserting that those subtypes are structurally identical as well.
func ErrIfMismatchedColumns(t1, t2 Type) error {
	if NumColumns(t1) != NumColumns(t2) {
		return ErrInvalidOperandColumns.New(NumColumns(t1), NumColumns(t2))
	}
	v1, ok1 := t1.(TupleType)
	v2, ok2 := t2.(TupleType)
	if ok1 && ok2 {
		for i := range v1 {
			if err := ErrIfMismatchedColumns(v1[i], v2[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrIfMismatchedColumnsInTuple returns an operand error is t2 is not a tuple
// type whose subtypes are structurally identical to t1.
func ErrIfMismatchedColumnsInTuple(t1, t2 Type) error {
	v2, ok2 := t2.(TupleType)
	if !ok2 {
		return ErrInvalidOperandColumns.New(NumColumns(t1), NumColumns(t2))
	}
	for _, v := range v2 {
		if err := ErrIfMismatchedColumns(t1, v); err != nil {
			return err
		}
	}
	return nil
}

// CompareNulls compares two values, and returns true if either is null.
// The returned integer represents the ordering, with a rule that states nulls
// as being ordered before non-nulls.
func CompareNulls(a interface{}, b interface{}) (bool, int) {
	aIsNull := a == nil
	bIsNull := b == nil
	if aIsNull && bIsNull {
		return true, 0
	} else if aIsNull && !bIsNull {
		return true, 1
	} else if !aIsNull && bIsNull {
		return true, -1
	}
	return false, 0
}
