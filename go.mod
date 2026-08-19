module repani.com/kv

go 1.25.0

require (
	golang.org/x/crypto v0.50.0
	golang.org/x/sys v0.43.0
	golang.org/x/term v0.42.0
)

require (
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

require repani.com/prim v0.0.0

replace repani.com/prim => ../prim
