# The release build makes mapping.txt for the crash traces (update.sh): the crash classes are kept
# under obfuscated names, so a trace of them needs the mapping.
-keep,allowobfuscation class navapp.crash.** { *; }
