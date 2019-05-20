package ipam

import "strings"

// escapeName removes any "/" from the name and URL encodes it to %2f,
// and necessarily removes % and encodes to %25.
func escapeName(name string) string {
	name = strings.Replace(name, "%", "%25", -1)
	return strings.Replace(name, "/", "%2f", -1)
}

// unescapeName replaces %2f and %25 in the name back to be a / and %.
func unescapeName(name string) string {
	name = strings.Replace(name, "%2f", "/", -1)
	return strings.Replace(name, "%25", "%", -1)
}
