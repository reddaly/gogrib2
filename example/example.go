package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/golang/glog"
	"github.com/sdifrance/gogrib2/gribio"
)

func main() {
	flag.Parse()
	if err := run(context.Background()); err != nil {
		glog.Exitf("got fatal error: %v", err)
	}
}

func run(_ context.Context) error {
	gribBytes, err := os.ReadFile("/usr/local/google/home/reddaly/tmp/ERA5_Land_Hourly_20221023_default_00.grib")
	if err != nil {
		return err
	}
	file, err := gribio.ReadFile(bytes.NewBuffer(gribBytes))
	if err != nil {
		return fmt.Errorf("error parsing grib file contents: %w", err)
	}
	for i, s := range file.GRIB1Messages() {
		glog.Infof("struct %d: %+v", i, s)
	}
	return nil
}
