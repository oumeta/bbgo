// Code generated by "callbackgen -type S4"; DO NOT EDIT.

package fmaker

import ()

func (inc *S4) OnUpdate(cb func(val float64)) {
	inc.UpdateCallbacks = append(inc.UpdateCallbacks, cb)
}

func (inc *S4) EmitUpdate(val float64) {
	for _, cb := range inc.UpdateCallbacks {
		cb(val)
	}
}