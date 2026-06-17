package app

import (
	"path"
	"strings"
)

func CleanPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return "/"
	}
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	if p == "." {
		return "/"
	}
	return p
}

func JoinUnderRoot(root, rel string) string {
	root = CleanPath(root)
	rel = CleanPath(rel)
	if rel == "/" {
		return root
	}
	if root == "/" {
		return rel
	}
	return CleanPath(root + "/" + strings.TrimPrefix(rel, "/"))
}

func RelativeToRoot(root, real string) string {
	root = CleanPath(root)
	real = CleanPath(real)
	if root != "/" && strings.HasPrefix(real, root) {
		v := strings.TrimPrefix(real, root)
		if v == "" {
			return "/"
		}
		return CleanPath(v)
	}
	return real
}

func ParentDir(p string) string {
	p = CleanPath(p)
	if p == "/" {
		return "/"
	}
	return path.Dir(p)
}

func BaseName(p string) string {
	return path.Base(CleanPath(p))
}
