package deps

import "yaria/internal/yaria/deps"

type Dep = deps.Dep

func DepsDir() string {
	return deps.DepsDir()
}

func CheckAll() []Dep {
	return deps.CheckAll()
}

func UpdateAll(progressFn func(name, status, msg string)) []Dep {
	return deps.UpdateAll(progressFn)
}
