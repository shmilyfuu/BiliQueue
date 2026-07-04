#!/usr/bin/env python3
from __future__ import annotations
import struct, sys
from pathlib import Path

RT_ICON = 3
RT_GROUP_ICON = 14
LANG_EN_US = 1033
IMAGE_RESOURCE_DIRECTORY_ENTRY_SUBDIR = 0x80000000


def align(n:int, a:int)->int:
    return (n + a - 1) // a * a

class ResourceBuilder:
    def __init__(self, section_rva:int):
        self.buf=bytearray()
        self.patch=[]
        self.data_items=[]
        self.section_rva=section_rva
    def tell(self): return len(self.buf)
    def add_dir(self, ids):
        # ids: list[(id, subdir_func or data_placeholder_index, is_subdir)]
        off=self.tell()
        self.buf += struct.pack('<IIHHHH',0,0,0,0,0,len(ids))
        entry_pos=[]
        for rid, target, is_subdir in ids:
            ep=self.tell(); entry_pos.append((ep,target,is_subdir))
            self.buf += struct.pack('<II', rid, 0)
        for ep,target,is_subdir in entry_pos:
            if is_subdir:
                child_off=target()
                value=child_off | IMAGE_RESOURCE_DIRECTORY_ENTRY_SUBDIR
            else:
                value=target()
            struct.pack_into('<I', self.buf, ep+4, value)
        return off
    def data_entry(self, data:bytes):
        idx=len(self.data_items)
        def write():
            off=self.tell()
            self.buf += struct.pack('<IIII',0, len(data), 0, 0)
            self.data_items.append((off,data))
            return off
        return write
    def finalize(self):
        # align data area to 4 and append data, patch data entry RVA
        while len(self.buf)%4: self.buf.append(0)
        for entry_off,data in self.data_items:
            data_off=len(self.buf)
            self.buf += data
            while len(self.buf)%4: self.buf.append(0)
            struct.pack_into('<I', self.buf, entry_off, self.section_rva + data_off)
        return bytes(self.buf)


def parse_ico(path:Path):
    b=path.read_bytes()
    reserved, typ, count = struct.unpack_from('<HHH', b, 0)
    if reserved != 0 or typ != 1 or count <= 0:
        raise ValueError('not an ICO file')
    images=[]
    for i in range(count):
        off=6+i*16
        width,height,color,reserved,planes,bitcount,size,imgoff=struct.unpack_from('<BBBBHHII', b, off)
        data=b[imgoff:imgoff+size]
        if len(data)!=size:
            raise ValueError('truncated icon image')
        images.append({
            'width': width, 'height': height, 'color': color, 'reserved': reserved,
            'planes': planes, 'bitcount': bitcount, 'size': size, 'data': data,
            'id': i+1,
        })
    return images


def group_icon_data(images):
    out=bytearray(struct.pack('<HHH',0,1,len(images)))
    for img in images:
        out += struct.pack('<BBBBHHIH', img['width'], img['height'], img['color'], 0, img['planes'], img['bitcount'], img['size'], img['id'])
    return bytes(out)


def build_resource(section_rva:int, ico_path:Path):
    images=parse_ico(ico_path)
    rb=ResourceBuilder(section_rva)
    icon_data_entries=[rb.data_entry(img['data']) for img in images]
    group_data_entry=rb.data_entry(group_icon_data(images))
    def icon_type_dir():
        entries=[]
        for img, data_ent in zip(images, icon_data_entries):
            def make_lang(de=data_ent):
                return lambda: rb.add_dir([(LANG_EN_US, de, False)])
            entries.append((img['id'], make_lang(), True))
        return rb.add_dir(entries)
    def group_type_dir():
        return rb.add_dir([(1, lambda: rb.add_dir([(LANG_EN_US, group_data_entry, False)]), True)])
    rb.add_dir([(RT_ICON, icon_type_dir, True), (RT_GROUP_ICON, group_type_dir, True)])
    return rb.finalize()


def patch_pe_icon(exe_path:Path, ico_path:Path, out_path:Path):
    b=bytearray(exe_path.read_bytes())
    if b[:2] != b'MZ': raise ValueError('not MZ')
    pe=struct.unpack_from('<I', b, 0x3c)[0]
    if b[pe:pe+4] != b'PE\0\0': raise ValueError('not PE')
    coff=pe+4
    nsec=struct.unpack_from('<H', b, coff+2)[0]
    szopt=struct.unpack_from('<H', b, coff+16)[0]
    opt=coff+20
    magic=struct.unpack_from('<H', b, opt)[0]
    if magic != 0x20b: raise ValueError('only PE32+ supported')
    section_alignment=struct.unpack_from('<I', b, opt+32)[0]
    file_alignment=struct.unpack_from('<I', b, opt+36)[0]
    data_dir=opt+112
    section_table=opt+szopt
    first_raw=min(struct.unpack_from('<I', b, section_table+i*40+20)[0] for i in range(nsec) if struct.unpack_from('<I', b, section_table+i*40+20)[0])
    if section_table+(nsec+1)*40 > first_raw:
        raise ValueError('not enough PE header room for a new section')
    max_end=0
    for i in range(nsec):
        off=section_table+i*40
        vs, va, rawsz, rawptr = struct.unpack_from('<IIII', b, off+8)
        max_end=max(max_end, va + align(max(vs, rawsz), section_alignment))
    new_rva=align(max_end, section_alignment)
    rsrc=build_resource(new_rva, ico_path)
    raw_ptr=align(len(b), file_alignment)
    if raw_ptr > len(b): b += b'\0'*(raw_ptr-len(b))
    raw_size=align(len(rsrc), file_alignment)
    b += rsrc + b'\0'*(raw_size-len(rsrc))
    # write new section header
    sh=section_table+nsec*40
    name=b'.rsrc\0\0\0'
    chars=0x40000040  # initialized data, readable
    b[sh:sh+40]=struct.pack('<8sIIIIIIHHI', name, len(rsrc), new_rva, raw_size, raw_ptr, 0, 0, 0, 0, chars)
    struct.pack_into('<H', b, coff+2, nsec+1)
    struct.pack_into('<I', b, opt+56, align(new_rva+len(rsrc), section_alignment))
    struct.pack_into('<II', b, data_dir+2*8, new_rva, len(rsrc))
    struct.pack_into('<I', b, opt+64, 0)  # checksum
    out_path.write_bytes(b)

if __name__ == '__main__':
    if len(sys.argv)!=4:
        print('usage: embed_icon_pe.py input.exe icon.ico output.exe', file=sys.stderr)
        sys.exit(2)
    patch_pe_icon(Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3]))
