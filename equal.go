package vela

import "reflect"

func equalValue(a, b any) bool { return reflect.DeepEqual(a, b) }
