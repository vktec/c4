package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Compiler struct {
	r *CompileResult

	blk  Block
	temp Temporary
	ret  bool // True if the last emitted instruction was `ret`

	loop []Loop              // Loop stack
	ns   []Namespace         // Namespace stack
	comp []CompositeLayout   // Composite types
	vars map[string]Variable // Local variable names
	strs []IRString          // String constants
	strM map[string]int      // Map from string to index of entry in strs
	data []Variable          // Global data
}

type CompileResult struct {
	typeW, codeW bytes.Buffer
}

func (r *CompileResult) String() string {
	return r.typeW.String() + r.codeW.String()
}
func (r *CompileResult) WriteTo(w io.Writer) (n int64, err error) {
	k, err := r.typeW.WriteTo(w)
	n += k
	if err != nil {
		return
	}

	k, err = r.codeW.WriteTo(w)
	n += k
	if err != nil {
		return
	}

	return
}

func NewCompiler() *Compiler {
	c := &Compiler{}
	c.r = &CompileResult{}
	c.ns = []Namespace{{
		"", map[string]Type{},
		map[string]*ConcreteType{},
	}}

	baseTypes := map[string]ConcreteType{
		"I64": TypeI64,
		"I32": TypeI32,
		"I16": TypeI16,
		"I8":  TypeI8,

		"U64": TypeU64,
		"U32": TypeU32,
		"U16": TypeU16,
		"U8":  TypeU8,

		"F64": TypeF64,
		"F32": TypeF32,

		"Bool": TypeBool,
	}
	for name, ty := range baseTypes {
		ty2 := ty // Copy so we can get a pointer to it
		c.ns[0].Typs[name] = &ty2
	}

	c.vars = map[string]Variable{}
	c.strM = map[string]int{}
	return c
}

func (c *Compiler) Compile(prog Program) (r *CompileResult, err error) {
	defer func() {
		switch e := recover().(type) {
		case nil:
		case string:
			err = errors.New(e)
		default:
			panic(e)
		}
	}()
	c.compile(prog)
	r = c.r
	c.r = &CompileResult{}
	return
}

func (c *Compiler) compile(prog Program) {
	prog.GenProgram(c)
	c.Finish()
}

func (c *Compiler) Writef(format string, args ...interface{}) {
	fmt.Fprintf(&c.r.codeW, format, args...)
}

func (c *Compiler) Insn(retVar Temporary, retType byte, opcode string, operands ...Operand) {
	// Skip all instructions after ret since they're unreachable
	if c.ret {
		return
	}

	b := &strings.Builder{}
	b.WriteString(opcode)
	for i, operand := range operands {
		if i > 0 {
			b.WriteRune(',')
		}
		b.WriteRune(' ')
		b.WriteString(operand.Operand())
	}

	if retVar.IsZero() {
		c.Writef("\t%s\n", b)
	} else {
		c.Writef("\t%s =%c %s\n", retVar, retType, b)
	}

	c.ret = opcode == "ret"
}

func (c *Compiler) StartNamespace(name string) {
	cur := c.NS()
	ns := Namespace{cur.Name + name + ".", map[string]Type{}, map[string]*ConcreteType{}}
	cur.Vars[name] = ns
	c.ns = append(c.ns, ns)
}
func (c *Compiler) EndNamespace() {
	c.ns = c.ns[:len(c.ns)-1]
	if len(c.ns) == 0 {
		panic("[compiler bug] End of global namespace")
	}
}
func (c *Compiler) NS() Namespace {
	return c.ns[len(c.ns)-1]
}

func (c *Compiler) StartFunction(export bool, name string, params []IRParam, retType string) {
	prefix := ""
	if export {
		prefix = "export "
	}

	pbuild := &strings.Builder{}
	ptemps := make([]Temporary, len(params))
	for i, param := range params {
		if i > 0 {
			pbuild.WriteString(", ")
		}

		ptype := param.Ty.IRTypeName(c)
		if ptype == "b" || ptype == "h" {
			ptype = "w"
		}
		pbuild.WriteString(ptype)

		pbuild.WriteRune(' ')
		ptemps[i] = c.Temporary()
		pbuild.WriteString(ptemps[i].Operand())
	}

	if retType == "b" || retType == "h" {
		retType = "w"
	}
	if retType != "" {
		retType += " "
	}
	name = c.NS().Name + name
	c.Writef("%sfunction %s$%s(%s) {\n@start\n", prefix, retType, name, pbuild)

	// Add args to locals
	for i, param := range params {
		loc := ptemps[i]
		if param.Ty.IRBaseTypeName() != 0 {
			// If it's a primitive, we need to alloc and copy
			loc = c.Temporary()
			c.allocLocal(loc, param.Ty)
			c.Insn(0, 0, "store"+param.Ty.IRTypeName(c), ptemps[i], loc)
		}
		c.vars[param.Name] = Variable{loc, param.Ty}
	}
}

func (c *Compiler) EndFunction() {
	if !c.ret {
		c.Insn(0, 0, "ret")
	}
	c.Writef("}\n")

	// Reset local information
	c.temp = 0
	c.blk = 0
	c.ret = false
	c.vars = map[string]Variable{}
}

type IRParam struct {
	Name string
	Ty   ConcreteType
}

func (c *Compiler) StartBlock(block Block) {
	c.Writef("%s\n", block)
	c.ret = false
}
func (c *Compiler) Block() Block {
	c.blk++
	return c.blk
}

func (c *Compiler) StartLoop(start, end Block) {
	c.loop = append(c.loop, Loop{start, end})
}
func (c *Compiler) EndLoop() {
	c.loop = c.loop[:len(c.loop)-1]
}
func (c *Compiler) Loop() Loop {
	return c.loop[len(c.loop)-1]
}

func (c *Compiler) Temporary() Temporary {
	c.temp++
	return c.temp
}

func (c *Compiler) AliasType(name string) *ConcreteType {
	cur := c.NS()
	if _, ok := cur.Typs[name]; ok {
		panic("Type already exists")
	}
	ty := new(ConcreteType)
	cur.Typs[name] = ty
	return ty
}
func (c *Compiler) Type(path ...string) ConcreteType {
	name, path := path[len(path)-1], path[:len(path)-1]

	if len(path) == 0 {
		if ty, ok := c.NS().Typs[name]; ok {
			return *ty
		}
	}

	ns := c.ns[0]
	for _, elem := range path {
		var ok bool
		if ns, ok = ns.Vars[elem].(Namespace); !ok {
			panic(elem + " is not a namespace")
		}
	}
	if ty, ok := ns.Typs[name]; ok {
		return *ty
	}

	panic("Unknown type: " + name)
}

func (c *Compiler) CompositeType(layout CompositeLayout) string {
	ident := layout.Ident()
	for i, layout_ := range c.comp {
		switch strings.Compare(layout_.Ident(), ident) {
		case 0:
			// Return
			return ident
		case 1:
			// Insert
			c.comp = append(c.comp, nil)
			copy(c.comp[i+1:], c.comp[i:])
			c.comp[i] = layout
			return ident
		}
	}
	// Append
	c.comp = append(c.comp, layout)
	return ident
}

func (c *Compiler) DeclareGlobal(extern bool, name string, ty ConcreteType) {
	cur := c.NS()
	if _, ok := cur.Vars[name]; ok {
		panic("Variable already exists")
	}
	cur.Vars[name] = ty
	if !extern {
		c.data = append(c.data, Variable{Global(cur.Name + name), ty})
	}
}
func (c *Compiler) allocLocal(loc Temporary, ty ConcreteType) {
	m := ty.Metrics()
	op := ""
	switch {
	case m.Align <= 4:
		op = "alloc4"
	case m.Align <= 8:
		op = "alloc8"
	case m.Align <= 16:
		op = "alloc16"
	default:
		panic("Invalid alignment")
	}
	c.Insn(loc, 'l', op, IRInt(m.Size))
}
func (c *Compiler) DeclareLocal(name string, ty ConcreteType) {
	if _, ok := c.vars[name]; ok {
		panic("Variable already exists")
	}
	loc := c.Temporary()
	c.vars[name] = Variable{loc, ty}

	c.allocLocal(loc, ty)
	ty.GenZero(c, loc)
}
func (c *Compiler) nsVar(i int, name string) (Variable, bool) {
	if ty, ok := c.ns[i].Vars[name]; ok {
		return Variable{Global(c.ns[i].Name + name), ty}, true
	}
	return Variable{}, false
}
func (c *Compiler) Variable(name string) Variable {
	if v, ok := c.vars[name]; ok {
		return v
	}
	if v, ok := c.nsVar(len(c.ns)-1, name); ok {
		return v
	}
	if v, ok := c.nsVar(0, name); ok {
		return v
	}
	panic("Undefined variable: " + name)
}

func (c *Compiler) String(str string) Global {
	i, ok := c.strM[str]
	if !ok {
		i = len(c.strs)
		c.strM[str] = i
		c.strs = append(c.strs, IRString(str))
	}
	return Global(fmt.Sprintf("str%d", i))
}

func (c *Compiler) Finish() {
	// Write composite types
	for _, layout := range c.comp {
		layout.GenType(c)
	}

	// Write strings
	for i, str := range c.strs {
		c.Writef("data $str%d = %s\n", i, str)
	}

	// Write global data
	for _, data := range c.data {
		m := data.Ty.Concrete().Metrics()
		c.Writef("data %s = align %d { z %d }\n", data.Loc, m.Align, m.Size)
	}
}

type CompositeLayout []CompositeEntry
type CompositeEntry struct {
	Ty string
	N  int
}

func (l CompositeLayout) Ident() string {
	b := &strings.Builder{}
	b.WriteByte(':')
	for _, entry := range l {
		if len(entry.Ty) > 1 {
			// X and Y act as parentheses
			b.WriteByte('X')
			b.WriteString(entry.Ty)
			b.WriteByte('Y')
		} else {
			b.WriteString(entry.Ty)
		}
		if entry.N > 1 {
			fmt.Fprintf(b, "%d", entry.N)
		}
	}
	return b.String()
}

func (l CompositeLayout) GenType(c *Compiler) {
	c.r.typeW.WriteString("type ")
	c.r.typeW.WriteString(l.Ident())
	c.r.typeW.WriteString(" = { ")
	for i, entry := range l {
		if i > 0 {
			c.r.typeW.WriteString(", ")
		}
		c.r.typeW.WriteString(entry.Ty)
		if entry.N > 1 {
			fmt.Fprintf(&c.r.typeW, " %d", entry.N)
		}
	}
	c.r.typeW.WriteString(" }\n")
}

type Operand interface {
	Operand() string
}

type Block uint

func (b Block) Operand() string {
	return fmt.Sprintf("@b%d", b)
}
func (b Block) String() string {
	return b.Operand()
}

type Loop struct {
	Start, End Block
}

type Temporary uint

func (t Temporary) IsZero() bool {
	return t == 0
}
func (t Temporary) Operand() string {
	return fmt.Sprintf("%%t%d", t)
}
func (t Temporary) String() string {
	return t.Operand()
}

type Global string

func (g Global) Operand() string {
	return "$" + string(g)
}
func (g Global) String() string {
	return g.Operand()
}

type IRInteger string

func IRInt(i int) IRInteger {
	return IRInteger(strconv.Itoa(i))
}
func (i IRInteger) Operand() string {
	return string(i)
}

type IRString string

func (s IRString) String() string {
	b := &strings.Builder{}
	b.WriteRune('{')
	inStr := false
	for i, ch := range append([]byte(s), 0) {
		if ' ' <= ch && ch <= '~' { // Printable ASCII range
			if !inStr {
				if i > 0 {
					b.WriteRune(',')
				}
				b.WriteString(` b "`)
				inStr = true
			}
			b.WriteByte(ch)
		} else {
			if inStr {
				b.WriteRune('"')
				inStr = false
			}
			if i > 0 {
				b.WriteRune(',')
			}
			fmt.Fprintf(b, " b %d", ch)
		}
	}
	b.WriteString(" }")
	return b.String()
}

type CallOperand struct {
	Var  bool // Variadic?
	Func Operand
	Args []TypedOperand
}
type TypedOperand struct {
	Ty string
	Op Operand
}

func (c CallOperand) Operand() string {
	b := &strings.Builder{}
	b.WriteString(c.Func.Operand())
	b.WriteRune('(')
	for i, arg := range c.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		if arg.Ty == "h" || arg.Ty == "b" {
			b.WriteString("w")
		} else {
			b.WriteString(arg.Ty)
		}
		b.WriteRune(' ')
		b.WriteString(arg.Op.Operand())
	}
	if c.Var {
		b.WriteString(", ...")
	}
	b.WriteRune(')')
	return b.String()
}

type Variable struct {
	Loc Operand
	Ty  Type
}
