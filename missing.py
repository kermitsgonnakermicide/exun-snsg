import json
import re
import subprocess
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from textual import work
from textual.app import App
from textual.containers import Horizontal, Vertical
from textual.widgets import Button, DataTable, Footer, Input, RichLog, Static

HERE = Path(__file__).parent
FIELD = re.compile(r"\*\([^)]*\)\s*\(?this(?:\s*\+\s*(0x[0-9a-fA-F]+|\d+))?\)?\s*=\s*(.+?);")


def normalize(code):
    code = re.sub(r"\b(FUN|DAT|LAB|UNK|PTR)_[0-9a-fA-F]+\b", r"\1", code)
    return re.sub(r"\s+", "", code)


def examine(binary, project, directory, ghidra):
    command = [ghidra, "--projects-dir", directory, "--project", project]

    def query(*arguments):
        result = subprocess.run(command + ["--json", *arguments], check=True, capture_output=True, text=True)
        return json.loads(result.stdout)

    try:
        query("import", str(binary))
        classes = {}
        for function in query("--program", binary.name, "function", "list", "--limit", "0"):
            signature = function.get("comment") or ""
            owner = re.match(r"([\w:]+)::[^:]+\(", signature)
            if not owner or owner[1].startswith("std::"):
                continue
            code = query("--program", binary.name, "decompile", "0x" + function["address"])[0]["code"]
            classes.setdefault(owner[1], {})[signature] = re.sub(r"/\*.*?\*/", "", code, flags=re.S).strip()
        return classes
    finally:
        subprocess.run(command + ["stop"], capture_output=True)


def properties(owner, methods):
    constructor = next((code for signature, code in methods.items() if signature.startswith(f"{owner}::{owner}(")), "")
    found = []
    for offset, value in FIELD.findall(constructor):
        if value.startswith('"'):
            kind, value = "string", json.loads(value)
        elif re.fullmatch(r"-?(0x[0-9a-fA-F]+|\d+)", value):
            kind, value = "integer", int(value, 0)
        elif value.startswith("param_"):
            kind, value = "argument", f"constructor argument {value[6:]}"
        else:
            kind = "expression"
        found.append({"offset": int(offset or "0", 0), "kind": kind, "value": value})
    return found


def analyze(original_path, altered_path, ghidra):
    with tempfile.TemporaryDirectory(prefix="missing-class-") as directory:
        original = examine(original_path, "original", directory, ghidra)
        altered = examine(altered_path, "altered", directory, ghidra)
    if not original or not altered:
        raise RuntimeError("fasiled class recovery -- check if correct binary is being used")
    ranking = []
    for owner, methods in original.items():
        changed = {
            signature: code for signature, code in methods.items()
            if normalize(code) != normalize(altered.get(owner, {}).get(signature, ""))
        }
        if changed:
            ranking.append({"class": owner, "changed": len(changed), "total": len(methods), "ratio": len(changed) / len(methods), "methods": changed})
    ranking.sort(key=lambda entry: (entry["ratio"], entry["changed"], entry["class"]), reverse=True)
    subject = ranking[0] if ranking else None
    return {
        "generated": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "original": str(original_path),
        "altered": str(altered_path),
        "classes": {"original": sorted(original), "altered": sorted(altered)},
        "ranking": [{key: entry[key] for key in ("class", "changed", "total", "ratio")} for entry in ranking],
        "missing": subject and {
            "class": subject["class"],
            "properties": properties(subject["class"], original[subject["class"]]),
            "methods": subject["methods"],
        },
    }


class Console(App):
    TITLE = "RECKONING ANOMALY ASSESSMENT SYSTEM"
    SUB_TITLE = "(C) 2026 Isengard Systems • "
    BINDINGS = [("f5", "scan", "Scan"), ("f9", "complete", "Complete analysis"), ("ctrl+q", "quit", "Quit")]
    CSS = """
    Screen { background: #0b0d0b; color: #c8d0b8; }
    .banner { background: #a8a23a; color: #0b0d0b; text-style: bold; width: 100%; content-align: center middle; }
    #header { height: 4; border: heavy #5d6b4a; padding: 0 1; color: #a8a23a; }
    #controls { width: 40; border: heavy #5d6b4a; border-title-color: #a8a23a; padding: 0 1; }
    #controls Input { background: #141814; border: tall #3a4430; margin-bottom: 1; }
    #controls Button { width: 100%; margin-bottom: 1; border: none; background: #2a3322; color: #c8d0b8; text-style: bold; }
    #controls Button:hover { background: #5d6b4a; }
    #complete { background: #6b2a1e; }
    #status { color: #a8a23a; }
    #main { border: heavy #5d6b4a; border-title-color: #a8a23a; }
    DataTable { height: 7; background: #0b0d0b; }
    #dossier { height: 1fr; border-top: heavy #3a4430; padding: 0 1; }
    RichLog { height: 8; border: heavy #5d6b4a; border-title-color: #a8a23a; background: #080908; }
    Footer { background: #141814; }
    """

    def compose(self):
        yield Static("AUTHORISED // OFFICIAL USE ONLY", classes="banner")
        yield Static(" (C)2026 Isengard Systems • Consult debrief for usage instructions •  ", id="header")
        with Horizontal():
            with Vertical(id="controls"):
                yield Static("REFERENCE BINARY")
                yield Input(str(HERE / "game"), id="original")
                yield Static("SUSPECT BINARY")
                yield Input(str(HERE / "game-altered"), id="altered")
                yield Static("GHIDRA CLI")
                yield Input(str(HERE / "ghidra"), id="ghidra")
                yield Button("F5 · SCAN", id="scan")
                yield Button("F9 · COMPLETE ANALYSIS", id="complete")
                yield Static("STATUS: STANDBY", id="status")
            with Vertical(id="main"):
                yield DataTable(cursor_type="row", zebra_stripes=True)
                yield Static("Awaiting Analysis", id="dossier")
        yield RichLog(markup=True, wrap=True)
        yield Footer()

    def on_mount(self):
        self.query_one("#controls").border_title = "TASKING"
        self.query_one("#main").border_title = "DIFFERENTIAL RESULTS"
        self.query_one(RichLog).border_title = "OPERATIONS LOG"
        self.query_one(DataTable).add_columns("CLASS", "ALTERED", "TOTAL", "DEVIATION")
        self.log_line("Succesful boot.")

    def log_line(self, text):
        self.query_one(RichLog).write(f"[#5d6b4a]{datetime.now():%H:%M:%S} [/]{text}")

    def on_button_pressed(self, event):
        self.run_analysis(export=event.button.id == "complete")

    def action_scan(self):
        self.run_analysis(export=False)

    def action_complete(self):
        self.run_analysis(export=True)

    @work(thread=True, exclusive=True)
    def run_analysis(self, export):
        paths = {key: Path(self.query_one(f"#{key}", Input).value).expanduser().resolve() for key in ("original", "altered", "ghidra")}
        self.call_from_thread(self.set_busy, True)
        self.call_from_thread(self.log_line, f"Beginning analysis of {paths['original'].name} and {paths['altered'].name}. This process will take time.")
        try:
            report = analyze(paths["original"], paths["altered"], str(paths["ghidra"]))
        except Exception as error:
            self.call_from_thread(self.log_line, f"[#c0503a]FAULT:[/] {error}")
        else:
            self.call_from_thread(self.show, report)
            if export:
                output = HERE / f"analysis-{datetime.now():%Y%m%d-%H%M%S}.json"
                output.write_text(json.dumps(report, indent=2))
                self.call_from_thread(self.log_line, f"Findings outputted: [b]{output.name}[/]")
        self.call_from_thread(self.set_busy, False)

    def set_busy(self, busy):
        for button in self.query(Button):
            button.disabled = busy
        self.query_one("#status", Static).update("STATUS: PROCESSING" if busy else "STATUS: STANDBY")

    def show(self, report):
        table = self.query_one(DataTable)
        table.clear()
        for entry in report["ranking"]:
            table.add_row(entry["class"], str(entry["changed"]), str(entry["total"]), f"{entry['ratio']:.0%}")
        missing = report["missing"]
        if not missing:
            self.query_one("#dossier", Static).update("NO DEVIATION DETECTED. BUILDS MATCH.")
            self.log_line("No missing or changed classes.")
            return
        lines = [f"[b #c0503a]SUBJECT MISSING: {missing['class'].upper()}[/] ", f"[b #c0503a] UPLOAD AND FOLLOW INCIDENT RESPONSE PROCEDURE 92 IMMEDIATELY", "", "[#a8a23a]RECOVERED ATTRIBUTES[/]"]
        lines += [f"  +0x{field['offset']:02X}  {field['kind'].upper():<10} {field['value']}" for field in missing["properties"]]
        lines += ["", "[#a8a23a]MISSING METHODS[/]"] + [f"  {signature}" for signature in missing["methods"]]
        self.query_one("#dossier", Static).update("\n".join(lines))
        self.log_line(f"[#c0503a]ALERT: [/]class {missing['class']} absent from suspect build.")


if __name__ == "__main__":
    Console().run()
