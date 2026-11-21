// Package codec is the JSON codec used for request binding and rendering.
//
// It is backed by bytedance/sonic in its standard-library compatible mode, so
// output is byte-for-byte what encoding/json would produce (sorted map keys,
// HTML escaping) while being considerably faster.
package codec

import "github.com/bytedance/sonic"

// Marshal encodes v as JSON.
func Marshal(v any) ([]byte, error) { return sonic.ConfigStd.Marshal(v) }

// Unmarshal decodes JSON data into v.
func Unmarshal(data []byte, v any) error { return sonic.ConfigStd.Unmarshal(data, v) }
