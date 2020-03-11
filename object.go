package goja

import (
	"fmt"
	"reflect"
	"runtime"
	"unsafe"
)

const (
	classObject   = "Object"
	classArray    = "Array"
	classWeakSet  = "WeakSet"
	classWeakMap  = "WeakMap"
	classMap      = "Map"
	classSet      = "Set"
	classFunction = "Function"
	classNumber   = "Number"
	classString   = "String"
	classBoolean  = "Boolean"
	classError    = "Error"
	classRegExp   = "RegExp"
	classDate     = "Date"
	classProxy    = "Proxy"

	classArrayIterator = "Array Iterator"
	classMapIterator   = "Map Iterator"
	classSetIterator   = "Set Iterator"
)

type weakCollection interface {
	removePtr(uintptr)
}

type weakCollections struct {
	colls []weakCollection
}

func (r *weakCollections) add(c weakCollection) {
	for _, ec := range r.colls {
		if ec == c {
			return
		}
	}
	r.colls = append(r.colls, c)
}

func (r *weakCollections) id() uintptr {
	return uintptr(unsafe.Pointer(r))
}

func (r *weakCollections) remove(c weakCollection) {
	if cap(r.colls) > 16 && cap(r.colls)>>2 > len(r.colls) {
		// shrink
		colls := make([]weakCollection, 0, len(r.colls))
		for _, coll := range r.colls {
			if coll != c {
				colls = append(colls, coll)
			}
		}
		r.colls = colls
	} else {
		for i, coll := range r.colls {
			if coll == c {
				l := len(r.colls) - 1
				r.colls[i] = r.colls[l]
				r.colls[l] = nil
				r.colls = r.colls[:l]
				break
			}
		}
	}
}

func finalizeObjectWeakRefs(r *weakCollections) {
	id := r.id()
	for _, c := range r.colls {
		c.removePtr(id)
	}
	r.colls = nil
}

type Object struct {
	runtime *Runtime
	self    objectImpl

	// Contains references to all weak collections that contain this Object.
	// weakColls has a finalizer that removes the Object's id from all weak collections.
	// The id is the weakColls pointer value converted to uintptr.
	// Note, cannot set the finalizer on the *Object itself because it's a part of a
	// reference cycle.
	weakColls *weakCollections
}

type iterNextFunc func() (propIterItem, iterNextFunc)

type PropertyDescriptor struct {
	jsDescriptor *Object

	Value Value

	Writable, Configurable, Enumerable Flag

	Getter, Setter Value
}

func(p PropertyDescriptor) toValue(r *Runtime) Value {
	if p.jsDescriptor != nil {
		return p.jsDescriptor
	}

	o := r.NewObject()
	s := o.self

	s._putProp("value", p.Value, false, false, false)

	s._putProp("writable", valueBool(p.Writable.Bool()), false, false, false)
	s._putProp("enumerable", valueBool(p.Enumerable.Bool()), false, false, false)
	s._putProp("configurable", valueBool(p.Configurable.Bool()), false, false, false)

	s._putProp("get", p.Getter, false, false, false)
	s._putProp("set", p.Setter, false, false, false)

	s.preventExtensions()

	return o
}

type objectImpl interface {
	sortable
	className() string
	get(Value) Value
	getProp(Value) Value
	getPropStr(string) Value
	getStr(string) Value
	getOwnProp(Value) Value
	getOwnPropStr(string) Value
	put(Value, Value, bool)
	putStr(string, Value, bool)
	hasProperty(Value) bool
	hasPropertyStr(string) bool
	hasOwnProperty(Value) bool
	hasOwnPropertyStr(string) bool
	_putProp(name string, value Value, writable, enumerable, configurable bool) Value
	defineOwnProperty(name Value, descr PropertyDescriptor, throw bool) bool
	toPrimitiveNumber() Value
	toPrimitiveString() Value
	toPrimitive() Value
	assertCallable() (call func(FunctionCall) Value, ok bool)
	deleteStr(name string, throw bool) bool
	delete(name Value, throw bool) bool
	proto() *Object
	setProto(proto *Object) *Object
	hasInstance(v Value) bool
	isExtensible() bool
	preventExtensions()
	enumerate(all, recursive bool) iterNextFunc
	_enumerate(recursive bool) iterNextFunc
	export() interface{}
	exportType() reflect.Type
	equal(objectImpl) bool
	getOwnSymbols() []Value
	getOwnPropertyDescriptor(name string) Value
}

type baseObject struct {
	class      string
	val        *Object
	prototype  *Object
	extensible bool

	values    map[string]Value
	propNames []string

	symValues map[*valueSymbol]Value
}

type primitiveValueObject struct {
	baseObject
	pValue Value
}

func (o *primitiveValueObject) export() interface{} {
	return o.pValue.Export()
}

func (o *primitiveValueObject) exportType() reflect.Type {
	return o.pValue.ExportType()
}

type FunctionCall struct {
	This      Value
	Arguments []Value
}

type ConstructorCall struct {
	This      *Object
	Arguments []Value
}

func (f FunctionCall) Argument(idx int) Value {
	if idx < len(f.Arguments) {
		return f.Arguments[idx]
	}
	return _undefined
}

func (f ConstructorCall) Argument(idx int) Value {
	if idx < len(f.Arguments) {
		return f.Arguments[idx]
	}
	return _undefined
}

func (o *baseObject) init() {
	o.values = make(map[string]Value)
}

func (o *baseObject) className() string {
	return o.class
}

func (o *baseObject) getPropStr(name string) Value {
	if val := o.getOwnPropStr(name); val != nil {
		return val
	}
	if o.prototype != nil {
		return o.prototype.self.getPropStr(name)
	}
	return nil
}

func (o *baseObject) getPropSym(s *valueSymbol) Value {
	if val := o.symValues[s]; val != nil {
		return val
	}
	if o.prototype != nil {
		return o.prototype.self.getProp(s)
	}
	return nil
}

func (o *baseObject) getProp(n Value) Value {
	if s, ok := n.(*valueSymbol); ok {
		return o.getPropSym(s)
	}
	return o.val.self.getPropStr(n.String())
}

func (o *baseObject) hasProperty(n Value) bool {
	return o.val.self.getProp(n) != nil
}

func (o *baseObject) hasPropertyStr(name string) bool {
	return o.val.self.getPropStr(name) != nil
}

func (o *baseObject) _getStr(name string) Value {
	p := o.getOwnPropStr(name)

	if p == nil && o.prototype != nil {
		p = o.prototype.self.getPropStr(name)
	}

	if p, ok := p.(*valueProperty); ok {
		return p.get(o.val)
	}

	return p
}

func (o *baseObject) getStr(name string) Value {
	p := o.val.self.getPropStr(name)
	if p, ok := p.(*valueProperty); ok {
		return p.get(o.val)
	}

	return p
}

func (o *baseObject) getSym(s *valueSymbol) Value {
	p := o.getPropSym(s)
	if p, ok := p.(*valueProperty); ok {
		return p.get(o.val)
	}

	return p
}

func (o *baseObject) get(n Value) Value {
	if s, ok := n.(*valueSymbol); ok {
		return o.getSym(s)
	}
	return o.getStr(n.String())
}

func (o *baseObject) checkDeleteProp(name string, prop *valueProperty, throw bool) bool {
	if !prop.configurable {
		o.val.runtime.typeErrorResult(throw, "Cannot delete property '%s' of %s", name, o.val.toString())
		return false
	}
	return true
}

func (o *baseObject) checkDelete(name string, val Value, throw bool) bool {
	if val, ok := val.(*valueProperty); ok {
		return o.checkDeleteProp(name, val, throw)
	}
	return true
}

func (o *baseObject) _delete(name string) {
	delete(o.values, name)
	for i, n := range o.propNames {
		if n == name {
			copy(o.propNames[i:], o.propNames[i+1:])
			o.propNames = o.propNames[:len(o.propNames)-1]
			break
		}
	}
}

func (o *baseObject) deleteStr(name string, throw bool) bool {
	if val, exists := o.values[name]; exists {
		if !o.checkDelete(name, val, throw) {
			return false
		}
		o._delete(name)
	}
	return true
}

func (o *baseObject) deleteSym(s *valueSymbol, throw bool) bool {
	if val, exists := o.symValues[s]; exists {
		if !o.checkDelete(s.String(), val, throw) {
			return false
		}
		delete(o.symValues, s)
	}
	return true
}

func (o *baseObject) delete(n Value, throw bool) bool {
	if s, ok := n.(*valueSymbol); ok {
		return o.deleteSym(s, throw)
	}
	return o.deleteStr(n.String(), throw)
}

func (o *baseObject) put(n Value, val Value, throw bool) {
	if s, ok := n.(*valueSymbol); ok {
		o.putSym(s, val, throw)
	} else {
		o.putStr(n.String(), val, throw)
	}
}

func (o *baseObject) getOwnPropStr(name string) Value {
	v := o.values[name]
	if v == nil && name == __proto__ {
		return o.prototype
	}
	return v
}

func (o *baseObject) getOwnProp(name Value) Value {
	if s, ok := name.(*valueSymbol); ok {
		return o.symValues[s]
	}

	return o.val.self.getOwnPropStr(name.String())
}

func (o *baseObject) setProto(proto *Object) *Object {
	current := o.prototype
	if current.SameAs(proto) {
		return nil
	}
	if !o.extensible {
		return o.val.runtime.NewTypeError("%s is not extensible", o.val)
	}
	for p := proto; p != nil; {
		if p.SameAs(o.val) {
			return o.val.runtime.NewTypeError("Cyclic __proto__ value")
		}
		p = p.self.proto()
	}
	o.prototype = proto
	return nil
}

func (o *baseObject) putStr(name string, val Value, throw bool) {
	if v, exists := o.values[name]; exists {
		if prop, ok := v.(*valueProperty); ok {
			if !prop.isWritable() {
				o.val.runtime.typeErrorResult(throw, "Cannot assign to read only property '%s'", name)
				return
			}
			prop.set(o.val, val)
			return
		}
		o.values[name] = val
		return
	}

	if name == __proto__ {
		var proto *Object
		if val != _null {
			if obj, ok := val.(*Object); ok {
				proto = obj
			} else {
				return
			}
		}
		if ex := o.setProto(proto); ex != nil {
			panic(ex)
		}
		return
	}

	var pprop Value
	if proto := o.prototype; proto != nil {
		pprop = proto.self.getPropStr(name)
	}

	if pprop != nil {
		if prop, ok := pprop.(*valueProperty); ok {
			if !prop.isWritable() {
				o.val.runtime.typeErrorResult(throw)
				return
			}
			if prop.accessor {
				prop.set(o.val, val)
				return
			}
		}
	} else {
		if !o.extensible {
			o.val.runtime.typeErrorResult(throw)
			return
		}
	}

	o.values[name] = val
	o.propNames = append(o.propNames, name)
}

func (o *baseObject) putSym(s *valueSymbol, val Value, throw bool) {
	if v, exists := o.symValues[s]; exists {
		if prop, ok := v.(*valueProperty); ok {
			if !prop.isWritable() {
				o.val.runtime.typeErrorResult(throw, "Cannot assign to read only property '%s'", s.String())
				return
			}
			prop.set(o.val, val)
			return
		}
		o.symValues[s] = val
		return
	}

	var pprop Value
	if proto := o.prototype; proto != nil {
		pprop = proto.self.getProp(s)
	}

	if pprop != nil {
		if prop, ok := pprop.(*valueProperty); ok {
			if !prop.isWritable() {
				o.val.runtime.typeErrorResult(throw)
				return
			}
			if prop.accessor {
				prop.set(o.val, val)
				return
			}
		}
	} else {
		if !o.extensible {
			o.val.runtime.typeErrorResult(throw)
			return
		}
	}

	if o.symValues == nil {
		o.symValues = make(map[*valueSymbol]Value, 1)
	}
	o.symValues[s] = val
}

func (o *baseObject) hasOwnProperty(n Value) bool {
	if s, ok := n.(*valueSymbol); ok {
		_, exists := o.symValues[s]
		return exists
	}
	v := o.values[n.String()]
	return v != nil
}

func (o *baseObject) hasOwnPropertyStr(name string) bool {
	v := o.values[name]
	return v != nil
}

func (o *baseObject) getOwnPropertyDescriptor(name string) Value {
	desc := o.getOwnProp(name)
	if desc == nil {
		return _undefined
	}
	var writable, configurable, enumerable, accessor bool
	var get, set *Object
	var value Value
	if v, ok := desc.(*valueProperty); ok {
		writable = v.writable
		configurable = v.configurable
		enumerable = v.enumerable
		accessor = v.accessor
		value = v.value
		get = v.getterFunc
		set = v.setterFunc
	} else {
		writable = true
		configurable = true
		enumerable = true
		value = desc
	}

	r := o.val.runtime
	ret := r.NewObject()
	obj := ret.self
	if !accessor {
		obj.putStr("value", value, false)
		obj.putStr("writable", r.toBoolean(writable), false)
	} else {
		if get != nil {
			obj.putStr("get", get, false)
		} else {
			obj.putStr("get", _undefined, false)
		}
		if set != nil {
			obj.putStr("set", set, false)
		} else {
			obj.putStr("set", _undefined, false)
		}
	}
	obj.putStr("enumerable", r.toBoolean(enumerable), false)
	obj.putStr("configurable", r.toBoolean(configurable), false)

	return ret
}

func (o *baseObject) _defineOwnProperty(name, existingValue Value, descr PropertyDescriptor, throw bool) (val Value, ok bool) {

	getterObj, _ := descr.Getter.(*Object)
	setterObj, _ := descr.Setter.(*Object)

	var existing *valueProperty

	if existingValue == nil {
		if !o.extensible {
			o.val.runtime.typeErrorResult(throw)
			return nil, false
		}
		existing = &valueProperty{}
	} else {
		if existing, ok = existingValue.(*valueProperty); !ok {
			existing = &valueProperty{
				writable:     true,
				enumerable:   true,
				configurable: true,
				value:        existingValue,
			}
		}

		if !existing.configurable {
			if descr.Configurable == FLAG_TRUE {
				goto Reject
			}
			if descr.Enumerable != FLAG_NOT_SET && descr.Enumerable.Bool() != existing.enumerable {
				goto Reject
			}
		}
		if existing.accessor && descr.Value != nil || !existing.accessor && (getterObj != nil || setterObj != nil) {
			if !existing.configurable {
				goto Reject
			}
		} else if !existing.accessor {
			if !existing.configurable {
				if !existing.writable {
					if descr.Writable == FLAG_TRUE {
						goto Reject
					}
					if descr.Value != nil && !descr.Value.SameAs(existing.value) {
						goto Reject
					}
				}
			}
		} else {
			if !existing.configurable {
				if descr.Getter != nil && existing.getterFunc != getterObj || descr.Setter != nil && existing.setterFunc != setterObj {
					goto Reject
				}
			}
		}
	}

	if descr.Writable == FLAG_TRUE && descr.Enumerable == FLAG_TRUE && descr.Configurable == FLAG_TRUE && descr.Value != nil {
		return descr.Value, true
	}

	if descr.Writable != FLAG_NOT_SET {
		existing.writable = descr.Writable.Bool()
	}
	if descr.Enumerable != FLAG_NOT_SET {
		existing.enumerable = descr.Enumerable.Bool()
	}
	if descr.Configurable != FLAG_NOT_SET {
		existing.configurable = descr.Configurable.Bool()
	}

	if descr.Value != nil {
		existing.value = descr.Value
		existing.getterFunc = nil
		existing.setterFunc = nil
	}

	if descr.Value != nil || descr.Writable != FLAG_NOT_SET {
		existing.accessor = false
	}

	if descr.Getter != nil {
		existing.getterFunc = propGetter(o.val, descr.Getter, o.val.runtime)
		existing.value = nil
		existing.accessor = true
	}

	if descr.Setter != nil {
		existing.setterFunc = propSetter(o.val, descr.Setter, o.val.runtime)
		existing.value = nil
		existing.accessor = true
	}

	if !existing.accessor && existing.value == nil {
		existing.value = _undefined
	}

	return existing, true

Reject:
	o.val.runtime.typeErrorResult(throw, "Cannot redefine property: %s", name.toString())
	return nil, false

}

func (o *baseObject) defineOwnProperty(n Value, descr PropertyDescriptor, throw bool) bool {
	n = toPropertyKey(n)
	if s, ok := n.(*valueSymbol); ok {
		existingVal := o.symValues[s]
		if v, ok := o._defineOwnProperty(n, existingVal, descr, throw); ok {
			if o.symValues == nil {
				o.symValues = make(map[*valueSymbol]Value, 1)
			}
			o.symValues[s] = v
			return true
		}
		return false
	}
	name := n.String()
	existingVal := o.values[name]
	if v, ok := o._defineOwnProperty(n, existingVal, descr, throw); ok {
		o.values[name] = v
		if existingVal == nil {
			o.propNames = append(o.propNames, name)
		}
		return true
	}
	return false
}

func (o *baseObject) _put(name string, v Value) {
	if _, exists := o.values[name]; !exists {
		o.propNames = append(o.propNames, name)
	}

	o.values[name] = v
}

func valueProp(value Value, writable, enumerable, configurable bool) Value {
	if writable && enumerable && configurable {
		return value
	}
	return &valueProperty{
		value:        value,
		writable:     writable,
		enumerable:   enumerable,
		configurable: configurable,
	}
}

func (o *baseObject) _putProp(name string, value Value, writable, enumerable, configurable bool) Value {
	prop := valueProp(value, writable, enumerable, configurable)
	o._put(name, prop)
	return prop
}

func (o *baseObject) tryExoticToPrimitive(hint string) Value {
	exoticToPrimitive := toMethod(o.getSym(symToPrimitive))
	if exoticToPrimitive != nil {
		return exoticToPrimitive(FunctionCall{
			This:      o.val,
			Arguments: []Value{newStringValue(hint)},
		})
	}
	return nil
}

func (o *baseObject) tryPrimitive(methodName string) Value {
	if method, ok := o.getStr(methodName).(*Object); ok {
		if call, ok := method.self.assertCallable(); ok {
			v := call(FunctionCall{
				This: o.val,
			})
			if _, fail := v.(*Object); !fail {
				return v
			}
		}
	}
	return nil
}

func (o *baseObject) toPrimitiveNumber() Value {
	if v := o.tryExoticToPrimitive("number"); v != nil {
		return v
	}

	if v := o.tryPrimitive("valueOf"); v != nil {
		return v
	}

	if v := o.tryPrimitive("toString"); v != nil {
		return v
	}

	o.val.runtime.typeErrorResult(true, "Could not convert %v to primitive", o)
	return nil
}

func (o *baseObject) toPrimitiveString() Value {
	if v := o.tryExoticToPrimitive("string"); v != nil {
		return v
	}

	if v := o.tryPrimitive("toString"); v != nil {
		return v
	}

	if v := o.tryPrimitive("valueOf"); v != nil {
		return v
	}

	o.val.runtime.typeErrorResult(true, "Could not convert %v to primitive", o)
	return nil
}

func (o *baseObject) toPrimitive() Value {
	return o.toPrimitiveNumber()
}

func (o *baseObject) assertCallable() (func(FunctionCall) Value, bool) {
	return nil, false
}

func (o *baseObject) proto() *Object {
	return o.prototype
}

func (o *baseObject) isExtensible() bool {
	return o.extensible
}

func (o *baseObject) preventExtensions() {
	o.extensible = false
}

func (o *baseObject) sortLen() int64 {
	return toLength(o.val.self.getStr("length"))
}

func (o *baseObject) sortGet(i int64) Value {
	return o.val.self.get(intToValue(i))
}

func (o *baseObject) swap(i, j int64) {
	ii := intToValue(i)
	jj := intToValue(j)

	x := o.val.self.get(ii)
	y := o.val.self.get(jj)

	o.val.self.put(ii, y, false)
	o.val.self.put(jj, x, false)
}

func (o *baseObject) export() interface{} {
	m := make(map[string]interface{})

	for item, f := o.enumerate(false, false)(); f != nil; item, f = f() {
		v := item.value
		if v == nil {
			v = o.getStr(item.name)
		}
		if v != nil {
			m[item.name] = v.Export()
		} else {
			m[item.name] = nil
		}
	}
	return m
}

func (o *baseObject) exportType() reflect.Type {
	return reflectTypeMap
}

type enumerableFlag int

const (
	_ENUM_UNKNOWN enumerableFlag = iota
	_ENUM_FALSE
	_ENUM_TRUE
)

type propIterItem struct {
	name       string
	value      Value // set only when enumerable == _ENUM_UNKNOWN
	enumerable enumerableFlag
}

type objectPropIter struct {
	o         *baseObject
	propNames []string
	recursive bool
	idx       int
}

type propFilterIter struct {
	wrapped iterNextFunc
	all     bool
	seen    map[string]bool
}

func (i *propFilterIter) next() (propIterItem, iterNextFunc) {
	for {
		var item propIterItem
		item, i.wrapped = i.wrapped()
		if i.wrapped == nil {
			return propIterItem{}, nil
		}

		if !i.seen[item.name] {
			i.seen[item.name] = true
			if !i.all {
				if item.enumerable == _ENUM_FALSE {
					continue
				}
				if item.enumerable == _ENUM_UNKNOWN {
					if prop, ok := item.value.(*valueProperty); ok {
						if !prop.enumerable {
							continue
						}
					}
				}
			}
			return item, i.next
		}
	}
}

func (i *objectPropIter) next() (propIterItem, iterNextFunc) {
	for i.idx < len(i.propNames) {
		name := i.propNames[i.idx]
		i.idx++
		prop := i.o.values[name]
		if prop != nil {
			return propIterItem{name: name, value: prop}, i.next
		}
	}

	if i.recursive && i.o.prototype != nil {
		return i.o.prototype.self._enumerate(i.recursive)()
	}
	return propIterItem{}, nil
}

func (o *baseObject) _enumerate(recursive bool) iterNextFunc {
	propNames := make([]string, len(o.propNames))
	copy(propNames, o.propNames)
	return (&objectPropIter{
		o:         o,
		propNames: propNames,
		recursive: recursive,
	}).next
}

func (o *baseObject) enumerate(all, recursive bool) iterNextFunc {
	return (&propFilterIter{
		wrapped: o._enumerate(recursive),
		all:     all,
		seen:    make(map[string]bool),
	}).next
}

func (o *baseObject) equal(objectImpl) bool {
	// Rely on parent reference comparison
	return false
}

func (o *baseObject) getOwnSymbols() (res []Value) {
	for s := range o.symValues {
		res = append(res, s)
	}

	return
}

func (o *baseObject) hasInstance(Value) bool {
	panic(o.val.runtime.NewTypeError("Expecting a function in instanceof check, but got %s", o.val.toString()))
}

func toMethod(v Value) func(FunctionCall) Value {
	if v == nil || IsUndefined(v) || IsNull(v) {
		return nil
	}
	if obj, ok := v.(*Object); ok {
		if call, ok := obj.self.assertCallable(); ok {
			return call
		}
	}
	panic(typeError(fmt.Sprintf("%s is not a method", v.String())))
}

func instanceOfOperator(o Value, c *Object) bool {
	if instOfHandler := toMethod(c.self.get(symHasInstance)); instOfHandler != nil {
		return instOfHandler(FunctionCall{
			This:      c,
			Arguments: []Value{o},
		}).ToBoolean()
	}

	return c.self.hasInstance(o)
}

func (o *Object) getWeakCollRefs() *weakCollections {
	if o.weakColls == nil {
		o.weakColls = &weakCollections{}
		runtime.SetFinalizer(o.weakColls, finalizeObjectWeakRefs)
	}

	return o.weakColls
}
