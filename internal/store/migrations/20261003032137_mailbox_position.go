package migrations

// Where a user put a folder among the ones beside it. Null is never placed: after the placed
// ones, in the usual order.
var mailboxPosition = Migration{
	Name: "20261003032137_mailbox_position",
	Up:   exec(`ALTER TABLE mailboxes ADD COLUMN position INTEGER;`),
}
