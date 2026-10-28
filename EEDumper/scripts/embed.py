Import("env")

from pathlib import Path

root = Path(env.subst("$PROJECT_DIR"))
source = root / "data" / "eeprom.bin"
if not source.is_file() or source.stat().st_size == 0:
    raise RuntimeError("Add a non-empty data/eeprom.bin before building")

build = Path(env.subst("$BUILD_DIR"))
build.mkdir(parents=True, exist_ok=True)
raw = source.read_bytes()
with (build / "eeprom_data.h").open("w") as out:
    out.write("#include <Arduino.h>\n#include <pgmspace.h>\nconst uint8_t eepromData[] PROGMEM = {\n")
    for start in range(0, len(raw), 16):
        out.write(",".join(f"0x{b:02X}" for b in raw[start:start + 16]) + ",\n")
    out.write("};\nconstexpr uint32_t eepromSize = sizeof(eepromData);\n")

env.Append(CPPPATH=[str(build)])
