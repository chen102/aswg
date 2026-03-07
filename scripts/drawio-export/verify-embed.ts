import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const repoRoot = process.argv[2]
  ? path.resolve(process.cwd(), process.argv[2])
  : path.resolve(__dirname, "..", "..");
const docsDir = path.join(repoRoot, "docs");
const diagramsDir = path.join(docsDir, "diagrams");
const exportedDir = path.join(diagramsDir, "exported");

if (!fs.existsSync(docsDir)) {
  throw new Error(`docs directory not found: ${docsDir}`);
}
if (!fs.existsSync(diagramsDir)) {
  throw new Error(`diagram directory not found: ${diagramsDir}`);
}
if (!fs.existsSync(exportedDir)) {
  throw new Error(`exported directory not found: ${exportedDir}`);
}

const drawioFiles = fs
  .readdirSync(diagramsDir)
  .filter((name) => name.toLowerCase().endsWith(".drawio"))
  .sort();
const markdownFiles = walkFiles(docsDir, (file) => file.toLowerCase().endsWith(".md"));

if (drawioFiles.length === 0) {
  console.log("No .drawio files found. Nothing to verify.");
  process.exit(0);
}

const markdownContents = markdownFiles.map((file) => ({
  file,
  content: fs.readFileSync(file, "utf8"),
}));

let hasError = false;
console.log(`Repo root: ${repoRoot}`);
console.log(`Diagrams: ${drawioFiles.length}, Markdown docs: ${markdownFiles.length}`);

for (const drawio of drawioFiles) {
  const base = drawio.replace(/\.drawio$/i, "");
  const svg = `${base}.svg`;
  const svgPath = path.join(exportedDir, svg);

  const svgExists = fs.existsSync(svgPath);
  const hasInlineEmbed = markdownContents.some(({ content }) => {
    const re = new RegExp(`!\\[[^\\]]*\\]\\(([^)]*${escapeRegExp(svg)})\\)`, "i");
    return re.test(content);
  });
  const hasSourceLink = markdownContents.some(({ content }) => {
    const re = new RegExp(`\\[[^\\]]*\\]\\(([^)]*${escapeRegExp(drawio)})\\)`, "i");
    return re.test(content);
  });

  if (!svgExists || !hasInlineEmbed || !hasSourceLink) {
    hasError = true;
  }

  console.log(
    [
      `[${svgExists ? "OK" : "ERR"}] svg:${svg}`,
      `[${hasInlineEmbed ? "OK" : "ERR"}] inline-embed`,
      `[${hasSourceLink ? "OK" : "ERR"}] source-link`,
    ].join(" | "),
  );
}

const absolutePathIssues: string[] = [];
for (const { file, content } of markdownContents) {
  if (/[A-Za-z]:\\\\/.test(content) || /\/Users\//.test(content) || /\/home\//.test(content)) {
    absolutePathIssues.push(path.relative(repoRoot, file));
  }
}

if (absolutePathIssues.length > 0) {
  hasError = true;
  console.log("\nFound absolute path leakage in markdown:");
  for (const file of absolutePathIssues) {
    console.log(`- ${file}`);
  }
}

if (hasError) {
  console.error("\nVerification failed. Fix the reported issues and run again.");
  process.exit(1);
}

console.log("\nVerification passed.");

function walkFiles(dir: string, filter: (file: string) => boolean): string[] {
  const result: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      result.push(...walkFiles(full, filter));
      continue;
    }
    if (entry.isFile() && filter(full)) {
      result.push(full);
    }
  }
  return result.sort();
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
