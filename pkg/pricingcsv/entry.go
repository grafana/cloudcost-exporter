package pricingcsv

import (
	"encoding/csv"
	"fmt"
	"os"
)

type Entry struct {
	Provider     string
	Service      string
	Region       string
	Zone         string
	InstanceType string
	CapacityType string
	StorageType  string
	Price        float64
	PricePerCore float64
	PricePerGiB  float64
}

func (e *Entry) ToSlice() []string {
	return []string{
		e.Provider,
		e.Service,
		e.Region,
		e.Zone,
		e.InstanceType,
		e.CapacityType,
		e.StorageType,
		fmt.Sprintf("%f", e.Price),
		fmt.Sprintf("%f", e.PricePerCore),
		fmt.Sprintf("%f", e.PricePerGiB),
	}
}

type CSVWriter struct {
	entries []*Entry
	file    *os.File
}

func NewCSVWriter(path string) (*CSVWriter, error) {
	outFile, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &CSVWriter{
		file: outFile,
	}, nil
}

func (c *CSVWriter) Close() error {
	_ = c.Flush()
	return c.file.Close()
}

func (c *CSVWriter) AddEntry(e *Entry) {
	c.entries = append(c.entries, e)
}

func (c *CSVWriter) Flush() error {
	writer := csv.NewWriter(c.file)
	defer writer.Flush()

	for _, entry := range c.entries {
		err := writer.Write(entry.ToSlice())
		if err != nil {
			return err
		}
	}

	c.entries = nil
	return nil
}
