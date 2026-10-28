missing.py requires:
Ghidra https://github.com/NationalSecurityAgency/ghidra (yes thats the actual NSA)
& https://github.com/akiselev/ghidra-cli
ensure ghidra-cli is on path/local directory
use uv for running missing.py
GHIDRA_INSTALL_DIR=/your/ghidra/install uv run missing.py
eg.
GHIDRA_INSTALL_DIR=/opt/ghidra uv run missing.py
fill in the initial & final binaries (you can use game & game-final) then press F9
