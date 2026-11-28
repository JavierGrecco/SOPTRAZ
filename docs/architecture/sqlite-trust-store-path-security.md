# Security hardening update — SQLite trust-store paths

The trust-store opener now resolves parent-directory aliases once and passes the canonical path to SQLite. It validates the resolved parent and pre-opens the database file with platform-specific no-follow semantics before SQLite is allowed to use the path.

- Linux/macOS: the final DB component is opened with `O_NOFOLLOW`, checked with `fstat` as a regular file, and protected with mode `0600`. A parent directory writable by group/others is rejected unless the sticky bit is set (for example, a shared temporary directory).
- Windows: the parent and final file are opened with `FILE_FLAG_OPEN_REPARSE_POINT`; directory/reparse-point substitutions are rejected. The DACL is read from the opened handle and broad write grants to Everyone, Authenticated Users, Builtin Users/Guests, Interactive, Network or Anonymous are rejected. No ACL is silently rewritten.
- Existing parent directories are not chmodded. If the policy rejects a custom path, the caller must select a directory with suitable permissions.
- The final no-follow preflight narrows path substitution risk. SQLite subsequently opens the canonical pathname independently; this is not a claim that a same-user process with write access to the parent cannot race that later open. Parent directory protection is therefore part of the security boundary.
- Windows ACL review is intentionally conservative: unfamiliar DACL forms or inaccessible security descriptors fail closed. Administrators/System and the current owner are not treated as broad principals; administrators and a privileged local attacker remain outside the local-user isolation claim.

Validation adds database symlink/directory rejection, canonical parent alias handling, Unix writable-parent and sticky-directory tests, and a Windows Everyone-writable DACL test.
