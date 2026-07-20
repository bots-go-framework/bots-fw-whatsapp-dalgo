# bots-fw-whatsapp-dalgo

DALgo implementations of the WhatsApp adapter's narrow persistence ports.
It preserves the `waSubjects`, `waChatData`, `waTemplates`, and
`waTemplateNames` collections and their original keys, so no existing WhatsApp
records need to be migrated.

Use the fixed-database constructors for a single database, or the corresponding
`WithProvider` constructors when the host selects DALgo from request or tenant
context. Database selection remains explicit; no package global is used.
