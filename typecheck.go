package main

func (e AssignExpr) TypeOf(c *Compiler) Type {
	ltyp, ok := e.L.TypeOf(c).(ConcreteType)
	if !ok {
		panic("Lvalue of non-concrete type")
	}
	rtyp := e.R.TypeOf(c)
	if !Compatible(ltyp, rtyp) {
		panic("Operands of assignment are incompatible")
	}
	return ltyp
}

func (e CallExpr) typeOf(c *Compiler) (t FuncType, ptr bool) {
	switch t := e.Func.TypeOf(c).(type) {
	case FuncType:
		return t, false
	case PointerType:
		return t.To.(FuncType), true
	}
	panic("Invalid function type")
}
func (e CallExpr) TypeOf(c *Compiler) Type {
	t, _ := e.typeOf(c)
	return t.Ret
}

func (e VarExpr) TypeOf(c *Compiler) Type {
	return c.Variable(string(e)).Ty
}

func (e RefExpr) TypeOf(c *Compiler) Type {
	return PointerType{e.V.TypeOf(c).(ConcreteType)}
}

func (e DerefExpr) TypeOf(c *Compiler) Type {
	if t, ok := e.V.TypeOf(c).(PointerType); ok {
		return t.To
	} else {
		panic("Dereference of non-pointer type")
	}
}

// FIXME: all operators other than add, sub, div and mul require integer types
// FIXME: lsh and rsh require their second argument to be an I32 or smaller
func (e BinaryExpr) TypeOf(c *Compiler) Type {
	ltyp := e.L.TypeOf(c)
	rtyp := e.R.TypeOf(c)
	if !Compatible(ltyp, rtyp) {
		panic("Operands of binary expression are incompatible")
	}
	ctyp := ltyp.Concrete()
	if !ltyp.IsConcrete() && rtyp.IsConcrete() {
		ctyp = rtyp.Concrete()
	}
	typ, ok := ctyp.(NumericType)
	if !ok {
		panic("Operand of binary expression is of non-numeric type")
	}
	return typ
}

func (_ IntegerExpr) TypeOf(c *Compiler) Type {
	return IntLitType{}
}
func (_ FloatExpr) TypeOf(c *Compiler) Type {
	return FloatLitType{}
}
func (_ StringExpr) TypeOf(c *Compiler) Type {
	// TODO: immutable types
	return PointerType{TypeI8}
}

func (name NamedTypeExpr) Get(c *Compiler) ConcreteType {
	return c.Type(string(name))
}
func (ptr PointerTypeExpr) Get(c *Compiler) ConcreteType {
	return PointerType{ptr.To.Get(c)}
}
func (fun FuncTypeExpr) Get(c *Compiler) ConcreteType {
	params := make([]ConcreteType, len(fun.Param))
	for i, param := range fun.Param {
		params[i] = param.Get(c)
	}
	var ret ConcreteType
	if fun.Ret != nil {
		ret = fun.Ret.Get(c)
	}
	return FuncType{params, ret}
}