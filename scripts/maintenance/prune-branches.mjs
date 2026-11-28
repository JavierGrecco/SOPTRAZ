import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const repository = process.env.REPOSITORY;
const defaultBranch = process.env.DEFAULT_BRANCH || "main";
const token = process.env.GITHUB_TOKEN;
const dryRun = process.env.DRY_RUN === "true";
const workflowRef = process.env.WORKFLOW_REF || "";

export function decideBranch(branch, {
  repository,
  defaultBranch,
  openRefs,
  mergedByRef,
  comparison,
}) {
  const name = branch.name;
  if (name === defaultBranch) return { delete: false, reason: "default branch" };
  if (branch.protected) return { delete: false, reason: "protected branch" };
  if (openRefs.has(name)) return { delete: false, reason: "open pull request" };
  if (!branch.commit?.sha) return { delete: false, reason: "branch SHA unavailable" };

  const mergedPRs = mergedByRef.get(name) || [];
  const exactMergedPR = mergedPRs.find(pr =>
    pr.head?.repo?.full_name === repository &&
    pr.head?.sha === branch.commit.sha
  );
  if (exactMergedPR) {
    return { delete: true, reason: `merged PR #${exactMergedPR.number} at exact head SHA` };
  }

  if (comparison?.ahead_by === 0 &&
      (comparison.status === "behind" || comparison.status === "identical")) {
    return { delete: true, reason: `no commits ahead of ${defaultBranch}` };
  }

  return { delete: false, reason: "unmerged or exclusive commits remain" };
}

function branchParam(name) {
  return encodeURIComponent(name);
}

async function main() {
  if (!repository || !token) throw new Error("Missing repository or GitHub token");
  if (workflowRef !== `refs/heads/${defaultBranch}`) {
    throw new Error(`Refusing to prune from non-default branch: ${workflowRef}`);
  }

  const api = `https://api.github.com/repos/${repository}`;
  const headers = {
    Authorization: `Bearer ${token}`,
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
  };

  async function request(path, options = {}) {
    const response = await fetch(`${api}${path}`, {
      ...options,
      headers: { ...headers, ...(options.headers || {}) },
    });
    if (response.status === 404 && options.allow404) return null;
    if (!response.ok) {
      const body = await response.text();
      throw new Error(`${options.method || "GET"} ${path}: HTTP ${response.status}: ${body}`);
    }
    if (response.status === 204) return null;
    const body = await response.text();
    return body ? JSON.parse(body) : null;
  }

  async function listAll(path) {
    const results = [];
    for (let page = 1; ; page += 1) {
      const separator = path.includes("?") ? "&" : "?";
      const batch = await request(`${path}${separator}per_page=100&page=${page}`);
      if (!Array.isArray(batch)) throw new Error(`Expected an array from ${path}`);
      results.push(...batch);
      if (batch.length < 100) return results;
    }
  }

  const [branches, pullRequests] = await Promise.all([
    listAll("/branches"),
    listAll("/pulls?state=all"),
  ]);
  const openRefs = new Set(
    pullRequests
      .filter(pr => pr.state === "open" && pr.head?.repo?.full_name === repository)
      .map(pr => pr.head.ref)
  );
  const mergedByRef = new Map();
  for (const pr of pullRequests) {
    if (pr.merged_at && pr.head?.repo?.full_name === repository) {
      const items = mergedByRef.get(pr.head.ref) || [];
      items.push(pr);
      mergedByRef.set(pr.head.ref, items);
    }
  }

  let deleted = 0;
  let skipped = 0;
  let dryRunCandidates = 0;

  for (const branch of branches) {
    const name = branch.name;
    const listedSHA = branch.commit?.sha;

    let comparison = null;
    const matchingMergedPR = (mergedByRef.get(name) || []).some(pr =>
      pr.head?.repo?.full_name === repository && pr.head?.sha === listedSHA
    );
    if (name !== defaultBranch && !branch.protected &&
        !openRefs.has(name) && listedSHA && !matchingMergedPR) {
      comparison = await request(
        `/compare/${encodeURIComponent(defaultBranch + "..." + name)}`
      );
    }

    const decision = decideBranch(branch, {
      repository,
      defaultBranch,
      openRefs,
      mergedByRef,
      comparison,
    });
    if (!decision.delete) {
      console.log(`KEEP ${name}: ${decision.reason}`);
      skipped += 1;
      continue;
    }

    // Re-read the branch immediately before deletion. GitHub's delete-ref API
    // has no expected-SHA lease, so a changed ref must always be preserved.
    const live = await request(`/branches/${branchParam(name)}`, { allow404: true });
    if (!live) {
      console.log(`SKIP ${name}: branch disappeared during cleanup`);
      skipped += 1;
      continue;
    }
    if (live.commit?.sha !== listedSHA || live.protected) {
      console.log(`KEEP ${name}: branch changed or became protected`);
      skipped += 1;
      continue;
    }

    // Refresh open PRs immediately before deleting to avoid removing a branch
    // that gained a new review request after the initial inventory.
    const liveOpenPRs = await listAll("/pulls?state=open");
    const hasOpenPR = liveOpenPRs.some(pr =>
      pr.head?.repo?.full_name === repository && pr.head.ref === name
    );
    if (hasOpenPR) {
      console.log(`KEEP ${name}: an open pull request appeared during cleanup`);
      skipped += 1;
      continue;
    }

    if (dryRun) {
      console.log(`DRY-RUN delete ${name} (${decision.reason})`);
      dryRunCandidates += 1;
      continue;
    }

    await request(`/git/refs/heads/${branchParam(name)}`, { method: "DELETE" });
    console.log(`DELETED ${name} (${decision.reason})`);
    deleted += 1;
  }

  console.log(
    `Branch hygiene finished: deleted=${deleted}, dry_run_candidates=${dryRunCandidates}, skipped=${skipped}, dry_run=${dryRun}`
  );
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main().catch(error => {
    console.error(error);
    process.exitCode = 1;
  });
}
