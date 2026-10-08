package fixtures

import "bytes"

// Derived variants reuse canonical declarations but get a distinct model
// identity. Guard the rewrite so upstream fixture drift cannot silently drop
// the only semantic declaration under test.
func replaceFixtureDocument(source []byte, before, after string) []byte {
	if bytes.Count(source, []byte(before)) != 1 {
		panic("derived canonical fixture needs exactly one matching declaration: " + before)
	}
	return bytes.Replace(source, []byte(before), []byte(after), 1)
}

func renamedFixtureDocument(source []byte, before, after string) []byte {
	return replaceFixtureDocument(source, "  - name: "+before+"\n", "  - name: "+after+"\n")
}
