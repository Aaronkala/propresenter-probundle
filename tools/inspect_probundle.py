#!/usr/bin/env python3
"""Inspect a ProPresenter .probundle: ZIP layout, manifest hashes, and .pro content.

Usage:
  inspect_probundle.py BUNDLE                 # layout + manifest check + song summary
  inspect_probundle.py BUNDLE --json          # full .pro as JSON (protobuf field names)
  inspect_probundle.py BUNDLE --extract DIR   # write all members to DIR
"""

import argparse
import base64
import io
import json
import re
import struct
import sys
import unicodedata
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "gen"))

import blake3  # noqa: E402
from google.protobuf import json_format  # noqa: E402

import presentation_pb2  # noqa: E402

MANIFEST = ".procore/legacy-export-manifest.json"


def open_bundle(path: Path) -> tuple[zipfile.ZipFile, list[str]]:
    """Open the ZIP. ProPresenter 21 writes a bad central-directory size; fix it in memory."""
    data = bytearray(path.read_bytes())
    notes = []
    eocd64 = data.rfind(b"PK\x06\x06")
    eocd = data.rfind(b"PK\x05\x06")
    if eocd64 != -1 and eocd != -1:
        claimed = struct.unpack_from("<Q", data, eocd64 + 40)[0]
        cd_offset = struct.unpack_from("<Q", data, eocd64 + 48)[0]
        real = eocd64 - cd_offset
        if claimed != real:
            notes.append(
                f"central directory size claims {claimed} bytes, real size is {real} "
                f"(+{claimed - real}: end records counted in); patched in memory"
            )
            struct.pack_into("<Q", data, eocd64 + 40, real)
            struct.pack_into("<I", data, eocd + 12, real)
    z = zipfile.ZipFile(io.BytesIO(bytes(data)))
    # Names are UTF-8 (NFD) but the UTF-8 flag (bit 11) is not set, so zipfile decodes them as cp437.
    for info in z.infolist():
        if not info.flag_bits & 0x800:
            info.filename = info.filename.encode("cp437").decode("utf-8")
    z.NameToInfo = {i.filename: i for i in z.infolist()}
    return z, notes


def rtf_to_text(rtf: str) -> str:
    """Enough RTF decoding for ProPresenter lyric slides (Cocoa RTF, cp1252 + \\u escapes)."""
    body = rtf[rtf.rfind("\\strokec") :] if "\\strokec" in rtf else rtf
    body = re.sub(r"^\\strokec\d+ ?", "", body)
    body = body.replace("\\\n", "\n")
    body = re.sub(r"\\'([0-9a-f]{2})", lambda m: bytes([int(m.group(1), 16)]).decode("cp1252"), body)
    body = re.sub(r"\\uc0\\u(\d+) ?", lambda m: chr(int(m.group(1))), body)
    body = re.sub(r"\\u(-?\d+) ?", lambda m: chr(int(m.group(1)) % 65536), body)
    return body.rstrip("}")


def summarize(pres) -> None:
    cues = {c.uuid.string: c for c in pres.cues}
    groups = {g.group.uuid.string: g for g in pres.cue_groups}
    print(f"presentation uuid : {pres.uuid.string}")
    print(f"name field        : {pres.name!r} (empty -> name comes from the .pro file name)")
    print(f"ccli              : title={pres.ccli.song_title!r} artist={pres.ccli.artist_credits!r} display={pres.ccli.display}")
    for arr in pres.arrangements:
        sel = " (selected)" if arr.uuid.string == pres.selected_arrangement.string else ""
        order = [groups[g.string].group.name for g in arr.group_identifiers]
        print(f"arrangement       : {arr.name!r}{sel}: {' > '.join(order)}")
    print(f"groups            : {len(pres.cue_groups)}  cues: {len(pres.cues)}")
    for g in pres.cue_groups:
        c = g.group.color
        lib = g.group.application_group_identifier.string
        print(f"\n[{g.group.name}] uuid={g.group.uuid.string} rgba=({c.red:.3f},{c.green:.3f},{c.blue:.3f},{c.alpha:.0f})"
              + (f" library-group={lib}" if lib else ""))
        for cid in g.cue_identifiers:
            cue = cues[cid.string]
            kinds = [pres.DESCRIPTOR.file.pool.FindEnumTypeByName("rv.data.Action.ActionType").values_by_number[a.type].name
                     for a in cue.actions]
            print(f"  cue {cid.string} actions={kinds}")
            for a in cue.actions:
                for el in a.slide.presentation.base_slide.elements:
                    if el.element.text.rtf_data:
                        text = rtf_to_text(el.element.text.rtf_data.decode("latin-1"))
                        for line in text.split("\n"):
                            print(f"    | {line}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("bundle", type=Path)
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--extract", type=Path)
    args = ap.parse_args()

    z, notes = open_bundle(args.bundle)
    pro_names = [i.filename for i in z.infolist() if i.filename.endswith(".pro")]

    if args.extract:
        z.extractall(args.extract)
        print(f"extracted {len(z.infolist())} members to {args.extract}")
        return 0
    if args.json:
        for name in pro_names:
            pres = presentation_pb2.Presentation()
            pres.ParseFromString(z.read(name))
            print(json_format.MessageToJson(pres, preserving_proto_field_name=True, indent=1))
        return 0

    print("== ZIP members ==")
    for n in notes:
        print(f"note: {n}")
    for i in z.infolist():
        nfd = " (NFD)" if i.filename != unicodedata.normalize("NFC", i.filename) else ""
        method = {0: "stored", 8: "deflate"}.get(i.compress_type, str(i.compress_type))
        print(f"  {i.file_size:>10}  {method:7}  flags=0x{i.flag_bits:x}  {i.filename}{nfd}")

    if MANIFEST in z.namelist():
        print("\n== manifest hash check ==")
        manifest = json.loads(z.read(MANIFEST))
        names = {unicodedata.normalize("NFC", n): n for n in z.namelist()}
        for entry in manifest["documents"] + manifest["media"]:
            member = names[unicodedata.normalize("NFC", entry["output_path"])]
            blob = z.read(member)
            ok = len(blob) == entry["byte_count"] and blake3.blake3(blob).hexdigest() == entry["blake3"]
            print(f"  {'OK ' if ok else 'BAD'} {entry['output_path']}")

    for name in pro_names:
        print(f"\n== {name} ==")
        raw = z.read(name)
        pres = presentation_pb2.Presentation()
        pres.ParseFromString(raw)
        print(f"protobuf round-trip byte-identical: {pres.SerializeToString() == raw}")
        summarize(pres)
    return 0


if __name__ == "__main__":
    sys.exit(main())
