#!/bin/sh -eux

packages=$(go list ./...)

go test $packages $@
