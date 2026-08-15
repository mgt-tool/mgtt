// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package providersupport

// Stdlib contains the 10 built-in primitive data types that all providers
// can reference using the "mgtt." namespace prefix.
var Stdlib = map[string]DataType{
	"int":        {Name: "int", Base: "int", Units: nil},
	"float":      {Name: "float", Base: "float", Units: nil},
	"bool":       {Name: "bool", Base: "bool", Units: nil},
	"string":     {Name: "string", Base: "string", Units: nil},
	"duration":   {Name: "duration", Base: "float", Units: []string{"ms", "s", "m", "h", "d"}, Range: &Range{Min: ptr(0.0)}},
	"bytes":      {Name: "bytes", Base: "int", Units: []string{"b", "kb", "mb", "gb", "tb"}, Range: &Range{Min: ptr(0.0)}},
	"ratio":      {Name: "ratio", Base: "float", Units: nil, Range: &Range{Min: ptr(0.0), Max: ptr(1.0)}},
	"percentage": {Name: "percentage", Base: "float", Units: nil, Range: &Range{Min: ptr(0.0), Max: ptr(100.0)}},
	"count":      {Name: "count", Base: "int", Units: nil, Range: &Range{Min: ptr(0.0)}},
	"timestamp":  {Name: "timestamp", Base: "string", Units: []string{"ISO8601"}},
}
