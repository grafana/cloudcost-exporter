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
