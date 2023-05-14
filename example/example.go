package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/sdifrance/gogrib2/grib1"

	"github.com/golang/glog"
	"github.com/sdifrance/gogrib2/gribio"
)

var (
	input = flag.String("input", "/usr/local/google/home/reddaly/tapestry/era5-land/ERA5_Land_Monthly_201901_default_00.grib", "Path to the input grib file.")
)

func main() {
	flag.Parse()
	if err := run(context.Background()); err != nil {
		glog.Exitf("got fatal error: %v", err)
	}
}

func run(_ context.Context) error {
	gribBytes, err := os.ReadFile(*input)
	if err != nil {
		return err
	}
	file, err := gribio.ReadFile(bytes.NewBuffer(gribBytes))
	if err != nil {
		return fmt.Errorf("error parsing grib file contents: %w", err)
	}

	weatherMessages, err := extractWeatherMessages(file)
	if err != nil {
		return err
	}

	for i, s := range file.GRIB1Messages() {
		glog.Infof("message[%d]: %+v", i, s)
	}
	glog.Infof("weather messages: %+v", weatherMessages)
	return nil
}

type weatherMessages struct {
	solar, windU, windV *grib1.Message
}

func extractWeatherMessages(file *gribio.File) (*weatherMessages, error) {
	out := &weatherMessages{
		solar: findByIndicatorOfParameter(file, grib1.ParameterIDSurfaceSolarRadiationDownwards),
		windU: findByIndicatorOfParameter(file, grib1.ParameterID10MeterUWindComponent),
		windV: findByIndicatorOfParameter(file, grib1.ParameterID10MeterVWindComponent),
	}
	if out.solar == nil {
		return nil, fmt.Errorf("missing solar radiation record (ParameterIDSurfaceSolarRadiationDownwards)")
	}
	if out.windU == nil {
		return nil, fmt.Errorf("missing wind U record (ParameterID10MeterUWindComponent)")
	}
	if out.windV == nil {
		return nil, fmt.Errorf("missing wind V record (ParameterID10MeterVWindComponent)")
	}
	return out, nil
}

func findByIndicatorOfParameter(file *gribio.File, id grib1.IndicatorOfParameter) *grib1.Message {
	return find(file.GRIB1Messages(), func(msg *grib1.Message) (*grib1.Message, bool) {
		if msg.ProductDefinition().IndicatorOfParameter() == id {
			return msg, true
		}
		return nil, false
	}, nil)
}

func find[E, R any](slice []E, predicate func(E) (R, bool), defaultOutput R) R {
	for _, e := range slice {
		if r, ok := predicate(e); ok {
			return r
		}
	}
	return defaultOutput
}
