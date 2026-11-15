# Configuration scopes in schema v2

[日本語](schema-v2_ja.md)

## Document contract

Schema v2 defines two document scopes. A repository document declares one
repository with `scope: repository` and uses the same repository settings
and paths as v1. A personal document declares `scope: personal`, the GitHub
login in `account`, and the optional `issueTypes` and `issueFields` lists.
The scopes are mutually exclusive. Both scopes accept YAML and JSON and
reject unknown keys and duplicate mappings. The existing schema entry point
accepts v1 and v2. The top-level `validate` command accepts both scopes.

GitHub makes personal issue definitions available in all repositories owned
by the account. The extension manages them through explicit `personal`
commands. It checks the authenticated login against the document's account
before reading or writing account settings. The repository commands use
repository configuration and endpoints. Repository import defaults to v1;
new exports can opt in to v2, and refresh keeps the destination version.

## Personal definitions

Issue types declare `name` and `isEnabled`, with optional `description` and
`color`. Issue fields declare `name` and `dataType`, with optional
`description`, `visibility` (`restricted` or `all`), and `options` for select
fields. The supported data types are `text`, `number`, `date`,
`single_select`, and `multi_select`. Each option declares `name`, `color`,
and integer `priority`, with an optional `description`. Names are unique
within a list, ignoring case. Each account can declare up to 25 types and 25
fields. A new select field requires options. GitHub's settings UI manages
field pinning and the order of pinned fields.

## Operations

The personal plan compares the declared settings with GitHub. The apply
command shows that plan and the impact on all owned repositories before
confirmation. It creates or updates definitions by name. It retains omitted
definitions, attributes, and options on existing definitions. A type mismatch
on an existing field or incomplete state blocks changes. When updating a
select field, the extension rereads the live field, preserves existing option
IDs, and includes undeclared options in the replacement request. Apply can
be rerun after a partial failure. The plan uses the same drift exit codes as
the repository command.

Personal import reads the authenticated account and emits a v2 document.
Refresh checks the destination account, updates declared collections, retains
entries that are absent on GitHub, and preserves YAML comments. Import needs
read access to personal Issue Types and Issue Fields. Apply needs write access
to the definitions it changes. Classic and OAuth credentials use `read:user`
for reads and `user` for writes. GitHub's repository workflow token cannot
administer a personal account.
