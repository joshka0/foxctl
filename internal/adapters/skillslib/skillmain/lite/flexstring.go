package lite

import (
	"encoding/json"
	"fmt"
)

// FlexString accepts both string and number JSON values, converting to string.
// Use this for fields like PR numbers that users might pass as either "123" or 123.
type FlexString string

// UnmarshalJSON implements json.Unmarshaler for FlexString.
func (f *FlexString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexString(n.String())
		return nil
	}
	return fmt.Errorf("FlexString: expected string or number, got %s", string(data))
}

// String returns the underlying string value.
func (f FlexString) String() string {
	return string(f)
}
