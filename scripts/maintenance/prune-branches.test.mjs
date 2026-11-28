import assert from "node:assert/strict";
import test from "node:test";
import { decideBranch } from "./prune-branches.mjs";

const repository = "JavierGrecco/SOPTRAZ";
const defaultBranch = "main";
const branch = (name = "feat/example", sha = "abc123", protectedBranch = false) => ({
  name,
  protected: protectedBranch,
  commit: { sha },
});
const context = overrides => ({
  repository,
  defaultBranch,
  openRefs: new Set(),
  mergedByRef: new Map(),
  comparison: null,
  ...overrides,
});

test("never deletes the default branch", () => {
  assert.deepEqual(
    decideBranch(branch("main"), context()),
    { delete: false, reason: "default branch" }
  );
});

test("never deletes a protected branch", () => {
  assert.deepEqual(
    decideBranch(branch("release/stable", "abc123", true), context()),
    { delete: false, reason: "protected branch" }
  );
});

test("never deletes a branch with an open pull request", () => {
  const options = context({ openRefs: new Set(["feat/example"]) });
  assert.deepEqual(
    decideBranch(branch(), options),
    { delete: false, reason: "open pull request" }
  );
});

test("deletes a merged PR branch only when its live SHA equals the merged head SHA", () => {
  const pr = {
    number: 31,
    head: { sha: "abc123", repo: { full_name: repository } },
  };
  const options = context({ mergedByRef: new Map([["feat/example", [pr]]]) });
  assert.equal(decideBranch(branch(), options).delete, true);
  assert.match(decideBranch(branch(), options).reason, /PR #31/);
});

test("does not delete a post-merge branch whose SHA changed and remains ahead", () => {
  const pr = {
    number: 31,
    head: { sha: "old123", repo: { full_name: repository } },
  };
  const options = context({
    mergedByRef: new Map([["feat/example", [pr]]]),
    comparison: { ahead_by: 1, status: "ahead" },
  });
  assert.equal(decideBranch(branch(), options).delete, false);
});

test("deletes an orphan branch only when it has no commits ahead of main", () => {
  const options = context({ comparison: { ahead_by: 0, status: "behind" } });
  assert.equal(decideBranch(branch(), options).delete, true);
});

test("keeps an orphan branch with exclusive commits", () => {
  const options = context({ comparison: { ahead_by: 2, status: "ahead" } });
  assert.deepEqual(
    decideBranch(branch(), options),
    { delete: false, reason: "unmerged or exclusive commits remain" }
  );
});

test("keeps a branch when the compare result is diverged", () => {
  const options = context({ comparison: { ahead_by: 0, status: "diverged" } });
  assert.equal(decideBranch(branch(), options).delete, false);
});

test("keeps branches without an available commit SHA", () => {
  assert.deepEqual(
    decideBranch({ name: "feat/unknown", protected: false, commit: {} }, context()),
    { delete: false, reason: "branch SHA unavailable" }
  );
});
