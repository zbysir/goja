package goja

import "sync"

type weakMap struct {
	// need to synchronise access to the data map because it may be accessed
	// from the finalizer goroutine
	sync.Mutex
	data map[uintptr]Value
}

type weakMapObject struct {
	baseObject
	m *weakMap
}

func newWeakMap() *weakMap {
	return &weakMap{
		data: make(map[uintptr]Value),
	}
}

func (wmo *weakMapObject) init() {
	wmo.baseObject.init()
	wmo.m = newWeakMap()
}

func (wm *weakMap) removePtr(ptr uintptr) {
	wm.Lock()
	delete(wm.data, ptr)
	wm.Unlock()
}

func (wm *weakMap) set(key *Object, value Value) {
	refs := key.getWeakCollRefs()
	wm.Lock()
	wm.data[refs.id()] = value
	wm.Unlock()
	refs.add(wm)
}

func (wm *weakMap) get(key *Object) Value {
	refs := key.weakColls
	if refs == nil {
		return nil
	}
	wm.Lock()
	ret := wm.data[refs.id()]
	wm.Unlock()
	return ret
}

func (wm *weakMap) remove(key *Object) bool {
	refs := key.weakColls
	if refs == nil {
		return false
	}
	id := refs.id()
	wm.Lock()
	_, exists := wm.data[id]
	if exists {
		delete(wm.data, id)
	}
	wm.Unlock()
	if exists {
		refs.remove(wm)
	}
	return exists
}

func (wm *weakMap) has(key *Object) bool {
	refs := key.weakColls
	if refs == nil {
		return false
	}
	id := refs.id()
	wm.Lock()
	_, exists := wm.data[id]
	wm.Unlock()
	return exists
}

func (r *Runtime) weakMapProto_delete(call FunctionCall) Value {
	thisObj := r.toObject(call.This)
	wmo, ok := thisObj.self.(*weakMapObject)
	if !ok {
		panic(r.NewTypeError("Method WeakMap.prototype.delete called on incompatible receiver %s", thisObj.String()))
	}
	key, ok := call.Argument(0).(*Object)
	if ok && wmo.m.remove(key) {
		return valueTrue
	}
	return valueFalse
}

func (r *Runtime) weakMapProto_get(call FunctionCall) Value {
	thisObj := r.toObject(call.This)
	wmo, ok := thisObj.self.(*weakMapObject)
	if !ok {
		panic(r.NewTypeError("Method WeakMap.prototype.get called on incompatible receiver %s", thisObj.String()))
	}
	var res Value
	if key, ok := call.Argument(0).(*Object); ok {
		res = wmo.m.get(key)
	}
	if res == nil {
		return _undefined
	}
	return res
}

func (r *Runtime) weakMapProto_has(call FunctionCall) Value {
	thisObj := r.toObject(call.This)
	wmo, ok := thisObj.self.(*weakMapObject)
	if !ok {
		panic(r.NewTypeError("Method WeakMap.prototype.has called on incompatible receiver %s", thisObj.String()))
	}
	key, ok := call.Argument(0).(*Object)
	if ok && wmo.m.has(key) {
		return valueTrue
	}
	return valueFalse
}

func (r *Runtime) weakMapProto_set(call FunctionCall) Value {
	thisObj := r.toObject(call.This)
	wmo, ok := thisObj.self.(*weakMapObject)
	if !ok {
		panic(r.NewTypeError("Method WeakMap.prototype.set called on incompatible receiver %s", thisObj.String()))
	}
	key := r.toObject(call.Argument(0))
	wmo.m.set(key, call.Argument(1))
	return call.This
}

func (r *Runtime) builtin_newWeakMap(args []Value) *Object {
	o := &Object{runtime: r}

	wmo := &weakMapObject{}
	wmo.class = classWeakMap
	wmo.val = o
	wmo.extensible = true
	o.self = wmo
	wmo.prototype = r.global.WeakMapPrototype
	wmo.init()
	if len(args) > 0 {
		if arg := args[0]; arg != nil && arg != _undefined && arg != _null {
			adder := wmo.getStr("set")
			iter := r.getIterator(arg, nil)
			i0 := intToValue(0)
			i1 := intToValue(1)
			if adder == r.global.weakMapAdder {
				r.iterate(iter, func(item Value) {
					itemObj := r.toObject(item)
					k := itemObj.self.get(i0)
					v := nilSafe(itemObj.self.get(i1))
					wmo.m.set(r.toObject(k), v)
				})
			} else {
				adderFn := toMethod(adder)
				if adderFn == nil {
					panic(r.NewTypeError("WeakMap.set in missing"))
				}
				r.iterate(iter, func(item Value) {
					itemObj := r.toObject(item)
					k := itemObj.self.get(i0)
					v := itemObj.self.get(i1)
					adderFn(FunctionCall{This: o, Arguments: []Value{k, v}})
				})
			}
		}
	}
	return o
}

func (r *Runtime) createWeakMapProto(val *Object) objectImpl {
	o := newBaseObjectObj(val, r.global.ObjectPrototype, classObject)

	o._putProp("constructor", r.global.WeakMap, true, false, true)
	r.global.weakMapAdder = r.newNativeFunc(r.weakMapProto_set, nil, "set", nil, 2)
	o._putProp("set", r.global.weakMapAdder, true, false, true)
	o._putProp("delete", r.newNativeFunc(r.weakMapProto_delete, nil, "delete", nil, 1), true, false, true)
	o._putProp("has", r.newNativeFunc(r.weakMapProto_has, nil, "has", nil, 1), true, false, true)
	o._putProp("get", r.newNativeFunc(r.weakMapProto_get, nil, "get", nil, 1), true, false, true)

	o.put(symToStringTag, valueProp(asciiString(classWeakMap), false, false, true), true)

	return o
}

func (r *Runtime) createWeakMap(val *Object) objectImpl {
	o := r.newNativeFuncObj(val, r.constructorThrower("WeakMap"), r.builtin_newWeakMap, "WeakMap", r.global.WeakMapPrototype, 0)

	return o
}

func (r *Runtime) initWeakMap() {
	r.global.WeakMapPrototype = r.newLazyObject(r.createWeakMapProto)
	r.global.WeakMap = r.newLazyObject(r.createWeakMap)

	r.addToGlobal("WeakMap", r.global.WeakMap)
}