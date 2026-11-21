package migrations

// Each user's own seal key: a salt for what their key is made from, and the key that seals their
// mail passwords, sealed with it. NULL until they make one. See docs/email-configs.md.
var sealKeys = Migration{
	Name: "20261121014808_seal_keys",
	Up: exec(`
ALTER TABLE users ADD COLUMN seal_salt BLOB;
ALTER TABLE users ADD COLUMN seal_key BLOB;
`),
}
