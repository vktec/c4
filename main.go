package main

import (
	"fmt"
	"os"
)

func main() {
	prog, err := Parse(`
	// Very amazing program to sum two inputted numbers
	fn puts(str [I8]) I32
	variadic fn scanf(fmt [I8]) I32
	variadic fn printf(fmt [I8]) I32
	pub fn main() I32 {
		puts("Enter two numbers:")

		var a, b I32
		printf("> ")
		scanf("%d", &a)
		printf("> ")
		scanf("%d", &b)

		var c I32
		var d [I32]
		d = &c
		[d] = 2 * (a + b)

		printf("2 * (%d + %d) = %d\n", a, b, c)
		return 0
	}
	`)

	if err != nil {
		fmt.Println("Parse error:", err)
	}

	NewCompiler(os.Stdout).Compile(prog)
}
