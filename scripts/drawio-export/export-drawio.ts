import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { DOMParser, XMLSerializer } from "@xmldom/xmldom";
import { convert, setTextMeasureProvider } from "@markdown-viewer/drawio2svg";
import iconv from "iconv-lite";

(globalThis as any).DOMParser = DOMParser;
(globalThis as any).XMLSerializer = XMLSerializer;

setTextMeasureProvider({
  measureText(text: string, fontSize: number) {
    const safeSize = Number.isFinite(fontSize) && fontSize > 0 ? fontSize : 12;
    const plain = (text ?? "").replace(/<[^>]+>/g, "");
    return {
      width: Math.max(1, plain.length) * safeSize * 0.55,
      height: safeSize * 1.2,
    };
  },
  measureTextLayout(
    text: string,
    fontSize: number,
    _fontFamily: string,
    _fontWeight: string,
    _fontStyle: string,
    containerWidth?: number,
    _isHtml?: boolean,
  ) {
    const safeSize = Number.isFinite(fontSize) && fontSize > 0 ? fontSize : 12;
    const plain = (text ?? "").replace(/<[^>]+>/g, "");
    const maxWidth = Number.isFinite(containerWidth as number) && (containerWidth as number) > 0
      ? (containerWidth as number)
      : safeSize * 20;
    const charWidth = safeSize * 0.55;
    const charsPerLine = Math.max(1, Math.floor(maxWidth / charWidth));
    const lineCount = Math.max(1, Math.ceil(plain.length / charsPerLine));
    const lineHeight = safeSize * 1.2;
    const width = Math.min(maxWidth, Math.max(charWidth, Math.min(plain.length, charsPerLine) * charWidth));
    const height = lineCount * lineHeight;
    return { width, height, lineCount, lineHeight };
  },
} as any);

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, "..", "..");

const sourceDir = process.argv[2]
  ? path.resolve(process.cwd(), process.argv[2])
  : path.join(repoRoot, "docs", "diagrams");
const outputDir = process.argv[3]
  ? path.resolve(process.cwd(), process.argv[3])
  : path.join(sourceDir, "exported");

if (!fs.existsSync(sourceDir)) {
  throw new Error(`Source directory not found: ${sourceDir}`);
}

fs.mkdirSync(outputDir, { recursive: true });

const drawioFiles = fs
  .readdirSync(sourceDir)
  .filter((name) => name.endsWith(".drawio"))
  .sort();

if (drawioFiles.length === 0) {
  console.log(`No .drawio files found in ${sourceDir}`);
  process.exit(0);
}

for (const name of drawioFiles) {
  const inFile = path.join(sourceDir, name);
  const outFile = path.join(outputDir, name.replace(/\.drawio$/i, ".svg"));
  const drawioXml = fs.readFileSync(inFile, "utf8");

  let svg = convert(drawioXml, {
    padding: 8,
    scale: 1,
    fontFamily: "Arial",
  });

  // Fix mojibake for CJK labels when converter text path uses a non-UTF codepage.
  svg = repairCjkGarbledText(drawioXml, svg);

  fs.writeFileSync(outFile, svg, "utf8");
  console.log(`Exported: ${name} -> ${path.relative(repoRoot, outFile)}`);
}

function repairCjkGarbledText(drawioXml: string, svg: string): string {
  const labels = extractCellValues(drawioXml);
  let repaired = svg;
  for (const label of labels) {
    if (!/[\u4e00-\u9fff]/.test(label)) continue;
    const garbled = iconv.decode(Buffer.from(label, "utf8"), "gbk");
    if (garbled !== label && repaired.includes(garbled)) {
      repaired = repaired.split(garbled).join(label);
    }
    const garbledWithQuestion = garbled.replace(/\uFFFD/g, "?");
    if (garbledWithQuestion !== label && repaired.includes(garbledWithQuestion)) {
      repaired = repaired.split(garbledWithQuestion).join(label);
    }
  }
  return repaired;
}

function extractCellValues(xml: string): string[] {
  const values: string[] = [];
  const regex = /<mxCell\b[^>]*\bvalue="([^"]*)"[^>]*>/g;
  let m: RegExpExecArray | null;
  while ((m = regex.exec(xml)) !== null) {
    const raw = m[1] ?? "";
    const value = decodeXmlAttr(raw).trim();
    if (value.length > 0) values.push(value);
  }
  return Array.from(new Set(values));
}

function decodeXmlAttr(s: string): string {
  return s
    .replace(/&quot;/g, "\"")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&amp;/g, "&")
    .replace(/&#10;/g, "\n")
    .replace(/&#13;/g, "\r");
}
