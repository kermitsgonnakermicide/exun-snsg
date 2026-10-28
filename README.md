# CONTRACT #72 - Skyseer
> Designed by Isengard Systems under contract for EXUN

Skyseer is a unified rapid-analysis platform in order to quickly classify occurences of EX. 732 in systems suspected to be compromised by EX. 732-1 instances. Skyseer provides two main tools in order to enhance field agent capabilities
- EEDumper
- CDiff

## EEDumper
EEDumper is a simple hardware for extracting stored data from EEPROM chips, characteristic of those used in arcades. It streams extracted binary files over USB for further analysis by CDiff

## CDiff
- Built by Experimental Design Bureau - 9 "Centurions", CDiff uses SOTA reverse-engineering tools to rapidly examine differences in pre & post incident files.
- CDiff was based off data provided to Isengard by Exun, with additional inputs from Isengard Special Information Operations (SIO)
- CDiff scans files for methods & signatures left inside compiled binaries, and then diffs them against the final binary, focusing on methods that have had major changes, and strings that no longer exist in the second binary
- CDiff is based on the simple premise that any incidents of EX. 732 will have caused noticible changes to the final binary, and thus focuses on heuristic checks that cut times upto 99.4% (1) as compared to manual review
(1) Per internal statistics

# Technology
- EEDumper uses a ESP32S3 + Arduino STL
- CDiff runs [Ghidra](https://github.com/NationalSecurityAgency/ghidra) with calls made to a  [CLI](https://github.com/akiselev/ghidra-cli). Frontend work is based off Textual for the TUI
