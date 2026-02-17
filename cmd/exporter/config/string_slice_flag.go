package config

import "strings"

type StringSliceFlag []string

func (f *StringSliceFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *StringSliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type DeprecatedStringSliceFlag struct {
	values  *StringSliceFlag
	wasUsed *bool
}

func NewDeprecatedStringSliceFlag(values *StringSliceFlag, wasUsed *bool) *DeprecatedStringSliceFlag {
	return &DeprecatedStringSliceFlag{
		values:  values,
		wasUsed: wasUsed,
	}
}

func (f *DeprecatedStringSliceFlag) String() string {
	return f.values.String()
}

func (f *DeprecatedStringSliceFlag) Set(value string) error {
	*f.wasUsed = true
	return f.values.Set(value)
}
