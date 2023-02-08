package main

type Obj interface {
	Close() error
	Symbols() []Symbol
}

type Symbol interface {
	Name() string
	Load(opt Options) *Code
}
