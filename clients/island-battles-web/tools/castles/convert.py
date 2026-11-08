"""Converts the Unity game's castle models to the web client's GLBs (issue #140).

Run it with Blender's command line:

    blender -b -P tools/castles/convert.py -- <path to an island-battles checkout>

or with Blender as a Python module (`bpy` on PyPI, CPython 3.13):

    uv run --python 3.13 --with bpy==5.2.3 python tools/castles/convert.py -- <checkout>

The checkout must be INTODAN/island-battles at UNITY_COMMIT; bumping it is a deliberate edit of
this file. It writes assets/castles/castle-level-{1..4}.glb and assets/castles/atlas.png next to
this tool's client, and prints each GLB's size and vertex and joint counts.

What it does, per level:
- imports Models/Castle_level_N.fbx, with its skins and its `Take 001` clip on levels 3 and 4,
  trimmed to the frames Unity plays (the clip settings in the .fbx.meta files);
- re-seats the big flags' cloth on its joints (see `seat_flag_cloth`);
- copies the per-polygon split between Unity's base and team materials from the same castle mesh
  in Models/Castle_all_team.fbx, failing if the meshes differ;
- exports two materials, `castle-base` (base faces) and `castle-team` (team faces, the big flags
  with their poles, and the windmill blades), with UVs and no texture;
- quantises the GLB's attributes with KHR_mesh_quantization (see `quantise_glb`);
- normalises: Y-up, the castle's footprint centred on the origin, its base at y = 0, and one scale
  for all four levels so that level 1 is LEVEL_1_ACROSS world units across.
"""

import json
import os
import struct
import subprocess
import sys

import bpy
import numpy
from mathutils import Matrix, Vector

UNITY_COMMIT = "c8b43b3a519d6bafdc8a3b4c5b1835ea2e2e1348"
ASSETS = "Island Battles Game/Assets"
LEVELS = (1, 2, 3, 4)
LEVEL_1_ACROSS = 0.9
# Unity plays frames 22 to 111 of `Take 001` at 24 fps on both levels (firstFrame and lastFrame in
# Models/Castle_level_3.fbx.meta and Castle_level_4.fbx.meta), and loops them.
CLIP_FRAMES = (22, 111)
CLIP_NAME = "Take 001"
# The castle body's width along x in Unity's units, as a check that the importer applied the
# files' mixed unit and node scales (levels 1 and 2: unit scale 100 and node scale 0.01; level 3:
# none; level 4: node scale 0.00642 and the prefab's x155.8).
EXPECTED_WIDTH = {1: 970, 2: 1039, 3: 1251, 4: 1288}
UNITY_BASE_MATERIAL = "Castle_basic_mat"
UNITY_TEAM_MATERIAL = "Castle_basic_Color_team_mat"


def fail(message):
    print(f"convert.py: {message}", file=sys.stderr)
    sys.exit(1)


def arguments():
    argv = sys.argv[sys.argv.index("--") + 1 :] if "--" in sys.argv else []
    if len(argv) != 1:
        fail("usage: convert.py -- <path to an island-battles checkout>")
    return os.path.abspath(argv[0])


def check_commit(checkout):
    def git(*args):
        return subprocess.run(
            ["git", "-C", checkout, *args], capture_output=True, text=True, check=True
        ).stdout.strip()

    try:
        head = git("rev-parse", "HEAD")
        changed = git("status", "--porcelain", "--", f"{ASSETS}/Models", f"{ASSETS}/Textures")
    except (OSError, subprocess.CalledProcessError) as error:
        fail(f"{checkout} is not a git checkout: {error}")
    if head != UNITY_COMMIT:
        fail(f"{checkout} is at {head}, not {UNITY_COMMIT}; check that commit out first")
    if changed:
        fail(f"{checkout} has local changes under Models or Textures:\n{changed}")


def reset():
    bpy.ops.wm.read_factory_settings(use_empty=True)
    bpy.context.scene.render.fps = 24


def import_fbx(path):
    # anim_offset 0 keeps the FBX frame numbers, so frame N is Unity's frame N.
    bpy.ops.import_scene.fbx(filepath=path, anim_offset=0.0)


def castle_body(level):
    """The level's castle mesh: the mesh with the most polygons."""
    meshes = [obj for obj in bpy.context.scene.objects if obj.type == "MESH"]
    body = max(meshes, key=lambda obj: len(obj.data.polygons))
    if not body.name.startswith(f"Castle_level_{level}"):
        fail(f"level {level}: the largest mesh is {body.name}, not the castle")
    return body


def team_split(checkout):
    """For each level, the castle mesh of Castle_all_team.fbx: its team flag per polygon, its
    polygons' vertex indices and its vertex positions."""
    reset()
    import_fbx(os.path.join(checkout, ASSETS, "Models", "Castle_all_team.fbx"))
    split = {}
    for level in LEVELS:
        candidates = [
            obj
            for obj in bpy.context.scene.objects
            if obj.type == "MESH" and obj.name.split(".")[0] == f"Castle_level_{level}"
        ]
        if len(candidates) != 1:
            fail(f"Castle_all_team.fbx: expected one mesh for level {level}")
        mesh = candidates[0].data
        names = [material.name if material else None for material in mesh.materials]
        if sorted(names) != sorted([UNITY_BASE_MATERIAL, UNITY_TEAM_MATERIAL]):
            fail(f"Castle_all_team.fbx level {level}: unexpected materials {names}")
        team = names.index(UNITY_TEAM_MATERIAL)
        split[level] = {
            "team": [polygon.material_index == team for polygon in mesh.polygons],
            "polygons": [tuple(polygon.vertices) for polygon in mesh.polygons],
            "vertices": [vertex.co.copy() for vertex in mesh.vertices],
            "width": world_width(candidates[0]),
        }
    return split


def make_material(name):
    material = bpy.data.materials.new(name)
    # Plain white with no texture: the client gives each material its atlas.
    material.diffuse_color = (1, 1, 1, 1)
    return material


def assign_materials(level, body, split, base, team):
    mesh = body.data
    source = split[level]
    if len(mesh.vertices) != len(source["vertices"]) or len(mesh.polygons) != len(source["team"]):
        fail(
            f"level {level}: the castle mesh has {len(mesh.vertices)} vertices and"
            f" {len(mesh.polygons)} polygons, Castle_all_team.fbx has"
            f" {len(source['vertices'])} and {len(source['team'])}"
        )
    for index, polygon in enumerate(mesh.polygons):
        if tuple(polygon.vertices) != source["polygons"][index]:
            fail(f"level {level}: polygon {index} differs from Castle_all_team.fbx")
    for index, vertex in enumerate(mesh.vertices):
        if (vertex.co - source["vertices"][index]).length > 1e-3:
            fail(f"level {level}: vertex {index} differs from Castle_all_team.fbx")
    mesh.materials.clear()
    mesh.materials.append(base)
    mesh.materials.append(team)
    for polygon, is_team in zip(mesh.polygons, source["team"]):
        polygon.material_index = 1 if is_team else 0
    return sum(source["team"]) / len(source["team"])


def seat_flag_cloth(obj):
    """Puts a skinned flag cloth back on its joints.

    Each big flag is a 45-vertex cloth with one joint per vertex, under parents with non-uniform
    scales (pCube35 and the skin's group46). Blender's FBX importer places the joints right but
    gives the cloth object a wrong transform, so the posed cloth lands hundreds of units away.
    In the file every vertex sits exactly on its joint at the bind pose, under one affine map of
    the cloth's own coordinates; this finds that map, writes the vertices in world space through
    it and makes the cloth's transform the identity, so the skin deforms it as Unity does.
    """
    armature = obj.find_armature()
    names = [group.name for group in obj.vertex_groups]
    local, world = [], []
    for vertex in obj.data.vertices:
        weights = [(group.weight, names[group.group]) for group in vertex.groups if group.weight > 0]
        if len(weights) != 1:
            fail(f"{obj.name}: vertex {vertex.index} has {len(weights)} joints, expected one")
        bone = armature.data.bones[weights[0][1]]
        local.append((*vertex.co, 1.0))
        world.append(tuple(armature.matrix_world @ bone.head_local))
    source = numpy.array(local)
    target = numpy.array(world)
    fit = numpy.linalg.lstsq(source, target, rcond=None)[0]
    error = numpy.abs(source @ fit - target).max()
    if error > 1e-3:
        fail(f"{obj.name}: the cloth's vertices are not on their joints (error {error:.4f})")
    for vertex, point in zip(obj.data.vertices, source @ fit):
        vertex.co = Vector(point)
    # The imported normals were for the old coordinates; let Blender recompute them.
    if obj.data.has_custom_normals:
        obj.data.normals_split_custom_set([(0.0, 0.0, 0.0)] * len(obj.data.loops))
    obj.matrix_world = Matrix.Identity(4)


def separate_by_material(body):
    """Splits the castle mesh into its base and team faces, as objects named `castle-base` and
    `castle-team`, so every mesh of the GLB says by its name which material it takes. (The client
    loads the GLB without its materials, which would otherwise pull in Babylon's PBR shaders.)"""
    for obj in bpy.context.scene.objects:
        obj.select_set(obj is body)
    bpy.context.view_layer.objects.active = body
    bpy.ops.object.mode_set(mode="EDIT")
    bpy.ops.mesh.select_all(action="SELECT")
    bpy.ops.mesh.separate(type="MATERIAL")
    bpy.ops.object.mode_set(mode="OBJECT")
    parts = [obj for obj in bpy.context.selected_objects if obj.type == "MESH"]
    if len(parts) != 2:
        fail(f"{body.name}: expected base and team faces, got {len(parts)} parts")
    for part in parts:
        if len(part.data.materials) > 1:
            # Separating leaves every slot on each part; keep only the one its faces use.
            used = {polygon.material_index for polygon in part.data.polygons}
            if len(used) != 1:
                fail(f"{part.name}: faces of two materials after separating")
            keep = part.data.materials[used.pop()]
            part.data.materials.clear()
            part.data.materials.append(keep)
        part.name = part.data.materials[0].name


def world_width(obj):
    low, high = world_bounds(obj)
    return high.x - low.x


def world_bounds(obj):
    corners = [obj.matrix_world @ Vector(vertex.co) for vertex in obj.data.vertices]
    low = Vector([min(corner[axis] for corner in corners) for axis in range(3)])
    high = Vector([max(corner[axis] for corner in corners) for axis in range(3)])
    return low, high


def trim_clip():
    scene = bpy.context.scene
    scene.frame_start, scene.frame_end = CLIP_FRAMES
    scene.frame_set(CLIP_FRAMES[0])


def glb_counts(path):
    """Vertices (summed over primitives) and joints (summed over skins) of a GLB."""
    with open(path, "rb") as file:
        data = file.read()
    length = struct.unpack_from("<I", data, 12)[0]
    document = json.loads(data[20 : 20 + length])
    accessors = document["accessors"]
    vertices = sum(
        accessors[primitive["attributes"]["POSITION"]]["count"]
        for mesh in document.get("meshes", [])
        for primitive in mesh["primitives"]
    )
    joints = sum(len(skin["joints"]) for skin in document.get("skins", []))
    animations = [
        (animation["name"], len(animation["channels"])) for animation in document.get("animations", [])
    ]
    return vertices, joints, animations


# ---- size: KHR_mesh_quantization -------------------------------------------------------------
#
# Blender writes 32-bit floats for every attribute. Without Draco or meshopt (whose decoders load
# from a CDN), the core-only way to shrink a GLB is KHR_mesh_quantization, which Babylon reads
# natively: normals as normalised bytes, UVs as normalised 16-bit integers, and the positions of
# meshes that are not skinned as normalised 16-bit integers, dequantised by a node of their own.
# It also drops animation channels that never leave the node's rest value. The skinned flag cloths
# keep float positions (45 vertices each).

COMPONENTS = {"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT4": 16}
DTYPES = {5120: "<i1", 5121: "<u1", 5122: "<i2", 5123: "<u2", 5125: "<u4", 5126: "<f4"}
ARRAY_BUFFER = 34962
ELEMENT_ARRAY_BUFFER = 34963


def read_glb(path):
    with open(path, "rb") as file:
        data = file.read()
    json_length = struct.unpack_from("<I", data, 12)[0]
    document = json.loads(data[20 : 20 + json_length])
    offset = 20 + json_length
    bin_length = struct.unpack_from("<I", data, offset)[0]
    return document, data[offset + 8 : offset + 8 + bin_length]


def write_glb(path, document, binary):
    text = json.dumps(document, separators=(",", ":")).encode()
    text += b" " * (-len(text) % 4)
    binary += b"\0" * (-len(binary) % 4)
    total = 12 + 8 + len(text) + 8 + len(binary)
    with open(path, "wb") as file:
        file.write(struct.pack("<4sII", b"glTF", 2, total))
        file.write(struct.pack("<I4s", len(text), b"JSON") + text)
        file.write(struct.pack("<I4s", len(binary), b"BIN\0") + binary)


def read_accessor(document, binary, index):
    accessor = document["accessors"][index]
    view = document["bufferViews"][accessor["bufferView"]]
    components = COMPONENTS[accessor["type"]]
    dtype = numpy.dtype(DTYPES[accessor["componentType"]])
    stride = view.get("byteStride", components * dtype.itemsize)
    start = view.get("byteOffset", 0) + accessor.get("byteOffset", 0)
    rows = [
        numpy.frombuffer(binary, dtype, components, start + row * stride) for row in range(accessor["count"])
    ]
    return numpy.array(rows, dtype=dtype).reshape(accessor["count"], components)


class Packer:
    """Collects accessors, each in a buffer view of its own, and lays out the new binary chunk."""

    def __init__(self):
        self.views, self.accessors, self.chunks, self.length = [], [], [], 0

    def add(self, values, accessor_type, component_type, target=None, normalized=False, bounds=False):
        dtype = numpy.dtype(DTYPES[component_type])
        values = numpy.ascontiguousarray(values, dtype=dtype)
        count, components = values.shape
        row = components * dtype.itemsize
        stride = row + (-row % 4) if target == ARRAY_BUFFER else row
        data = numpy.zeros((count, stride), dtype=numpy.uint8)
        data[:, :row] = values.view(numpy.uint8).reshape(count, row)
        self.length += -self.length % 4
        view = {"buffer": 0, "byteOffset": self.length, "byteLength": count * stride}
        if target == ARRAY_BUFFER:
            view["byteStride"] = stride
        if target is not None:
            view["target"] = target
        self.chunks.append((self.length, data.tobytes()))
        self.length += count * stride
        self.views.append(view)
        accessor = {
            "bufferView": len(self.views) - 1,
            "componentType": component_type,
            "count": count,
            "type": accessor_type,
        }
        if normalized:
            accessor["normalized"] = True
        if bounds:
            cast = int if dtype.kind in "iu" else float
            accessor["min"] = [cast(value) for value in values.min(axis=0)]
            accessor["max"] = [cast(value) for value in values.max(axis=0)]
        self.accessors.append(accessor)
        return len(self.accessors) - 1

    def binary(self):
        out = bytearray(self.length)
        for offset, data in self.chunks:
            out[offset : offset + len(data)] = data
        return bytes(out)


REST = {"translation": [0, 0, 0], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}


def quantise_glb(path):
    document, binary = read_glb(path)
    nodes = document["nodes"]
    packer = Packer()
    copied = {}

    def copy(index, target=None):
        """The accessor as it was, in the new layout; accessors with the same contents (such as
        the key times of every channel) are written once."""
        accessor = document["accessors"][index]
        values = read_accessor(document, binary, index)
        key = (
            values.tobytes(),
            values.shape,
            accessor["type"],
            accessor["componentType"],
            target,
            accessor.get("normalized", False),
        )
        if key not in copied:
            copied[key] = packer.add(
                values,
                accessor["type"],
                accessor["componentType"],
                target,
                accessor.get("normalized", False),
                "min" in accessor,
            )
        return copied[key]

    # Animations: drop channels that hold the node's rest value all the way.
    for animation in document.get("animations", []):
        kept_channels, kept_samplers = [], []
        for channel in animation["channels"]:
            sampler = animation["samplers"][channel["sampler"]]
            target = channel["target"]
            values = read_accessor(document, binary, sampler["output"]).astype(numpy.float64)
            rest = nodes[target["node"]].get(target["path"], REST.get(target["path"]))
            if rest is not None and numpy.abs(values - numpy.array(rest)).max() < 1e-5:
                continue
            kept_samplers.append(
                {
                    **sampler,
                    "input": copy(sampler["input"]),
                    "output": copy(sampler["output"]),
                }
            )
            kept_channels.append({**channel, "sampler": len(kept_samplers) - 1})
        animation["channels"], animation["samplers"] = kept_channels, kept_samplers

    for skin in document.get("skins", []):
        skin["inverseBindMatrices"] = copy(skin["inverseBindMatrices"])

    users = {}
    for index, node in enumerate(nodes):
        if "mesh" in node:
            users.setdefault(node["mesh"], []).append(index)

    for mesh_index, mesh in enumerate(document.get("meshes", [])):
        owners = users.get(mesh_index, [])
        skinned = any("skin" in nodes[owner] for owner in owners)
        quantise_positions = len(owners) == 1 and not skinned and "children" not in mesh
        positions = [
            read_accessor(document, binary, primitive["attributes"]["POSITION"]).astype(numpy.float64)
            for primitive in mesh["primitives"]
        ]
        every = numpy.concatenate(positions)
        low, high = every.min(axis=0), every.max(axis=0)
        centre = (low + high) / 2
        half = float(max(high - low)) / 2 or 1.0
        for primitive, position in zip(mesh["primitives"], positions):
            attributes = primitive["attributes"]
            for name, index in list(attributes.items()):
                values = read_accessor(document, binary, index)
                if name == "POSITION" and quantise_positions:
                    quantised = numpy.round((position - centre) / half * 32767)
                    attributes[name] = packer.add(quantised, "VEC3", 5122, ARRAY_BUFFER, True, True)
                elif name == "NORMAL":
                    quantised = numpy.round(numpy.clip(values, -1, 1) * 127)
                    attributes[name] = packer.add(quantised, "VEC3", 5120, ARRAY_BUFFER, True)
                elif name.startswith("TEXCOORD") and values.min() >= 0 and values.max() <= 1:
                    quantised = numpy.round(values.astype(numpy.float64) * 65535)
                    attributes[name] = packer.add(quantised, "VEC2", 5123, ARRAY_BUFFER, True)
                else:
                    attributes[name] = copy(index, ARRAY_BUFFER)
            if "indices" in primitive:
                primitive["indices"] = copy(primitive["indices"], ELEMENT_ARRAY_BUFFER)
        if quantise_positions:
            # The mesh moves to a child node that turns its integers back into positions.
            owner = nodes[owners[0]]
            del owner["mesh"]
            nodes.append(
                {
                    "name": f"{owner.get('name', 'mesh')}-dequantise",
                    "mesh": mesh_index,
                    "translation": [float(value) for value in centre],
                    "scale": [half] * 3,
                }
            )
            owner.setdefault("children", []).append(len(nodes) - 1)

    document["accessors"] = packer.accessors
    document["bufferViews"] = packer.views
    out = packer.binary()
    document["buffers"] = [{"byteLength": len(out) + (-len(out) % 4)}]
    for key in ("extensionsUsed", "extensionsRequired"):
        document[key] = sorted(set(document.get(key, [])) | {"KHR_mesh_quantization"})
    write_glb(path, document, out)


def convert_level(checkout, level, split, scale, out_dir):
    reset()
    import_fbx(os.path.join(checkout, ASSETS, "Models", f"Castle_level_{level}.fbx"))
    body = castle_body(level)
    base = make_material("castle-base")
    team = make_material("castle-team")
    share = assign_materials(level, body, split, base, team)
    # Everything else in the level file is the big flags with their poles, and the blades.
    for obj in list(bpy.context.scene.objects):
        if obj.type == "MESH" and obj is not body:
            obj.data.materials.clear()
            obj.data.materials.append(team)
            if obj.find_armature():
                seat_flag_cloth(obj)
            obj.name = f"castle-team-{obj.name}"
    for material in list(bpy.data.materials):
        if material not in (base, team):
            bpy.data.materials.remove(material)

    low, high = world_bounds(body)
    width = high.x - low.x
    expected = EXPECTED_WIDTH[level] / 100
    if abs(width / expected - 1) > 0.01:
        fail(f"level {level}: the castle is {width:.3f} wide, expected {expected:.3f}")
    separate_by_material(body)

    # One root for the whole model: it centres the footprint (Blender's x and y), puts the base at
    # zero and applies the shared scale. The exporter turns Blender's Z-up into glTF's Y-up.
    root = bpy.data.objects.new(f"castle-level-{level}", None)
    bpy.context.scene.collection.objects.link(root)
    for obj in list(bpy.context.scene.objects):
        if obj is not root and obj.parent is None:
            obj.parent = root
    centre = (low + high) / 2
    root.scale = (scale, scale, scale)
    root.location = (-centre.x * scale, -centre.y * scale, -low.z * scale)

    animated = level >= 3
    if animated:
        trim_clip()
    path = os.path.join(out_dir, f"castle-level-{level}.glb")
    bpy.ops.export_scene.gltf(
        filepath=path,
        export_format="GLB",
        export_yup=True,
        export_apply=False,
        export_texcoords=True,
        export_normals=True,
        export_tangents=False,
        export_materials="EXPORT",
        export_vertex_color="NONE",
        export_attributes=False,
        export_cameras=False,
        export_lights=False,
        export_extras=False,
        export_skins=animated,
        export_all_influences=False,
        export_morph=False,
        export_animations=animated,
        export_animation_mode="ACTIVE_ACTIONS",
        export_nla_strips_merged_animation_name=CLIP_NAME,
        export_frame_range=True,
        export_anim_slide_to_zero=True,
        export_force_sampling=True,
        export_frame_step=2,
        export_optimize_animation_size=True,
        export_anim_single_armature=False,
        export_draco_mesh_compression_enable=False,
        export_meshopt_compression_enable=False,
        export_use_gltfpack=False,
        export_copyright="",
    )
    quantise_glb(path)
    size = os.path.getsize(path)
    vertices, joints, animations = glb_counts(path)
    height = (high.z - low.z) * scale
    print(
        f"castle-level-{level}.glb: {size} bytes, {vertices} vertices, {joints} joints,"
        f" animations {animations}, team faces {share:.0%},"
        f" {width * scale:.3f} across, {height:.3f} high"
    )
    return size


def copy_atlas(checkout, out_dir):
    """Copies the base atlas with its pixels untouched, dropping its colour profile and metadata
    (an Adobe RGB iCCP chunk and XMP): Unity used the raw values, and a browser would otherwise
    convert them."""
    with open(os.path.join(checkout, ASSETS, "Textures", "Castles", "basic_mat_color.png"), "rb") as file:
        data = file.read()
    signature = b"\x89PNG\r\n\x1a\n"
    if not data.startswith(signature):
        fail("basic_mat_color.png is not a PNG")
    output = [signature]
    offset = len(signature)
    while offset < len(data):
        length = struct.unpack_from(">I", data, offset)[0]
        kind = data[offset + 4 : offset + 8]
        if kind in (b"IHDR", b"IDAT", b"IEND"):
            output.append(data[offset : offset + 12 + length])
        offset += 12 + length
    path = os.path.join(out_dir, "atlas.png")
    with open(path, "wb") as file:
        file.write(b"".join(output))
    print(f"atlas.png: {os.path.getsize(path)} bytes")


def main():
    checkout = arguments()
    check_commit(checkout)
    out_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "assets", "castles")
    out_dir = os.path.normpath(out_dir)
    os.makedirs(out_dir, exist_ok=True)
    split = team_split(checkout)
    scale = LEVEL_1_ACROSS / split[1]["width"]
    total = sum(convert_level(checkout, level, split, scale, out_dir) for level in LEVELS)
    print(f"total: {total} bytes in four GLBs")
    copy_atlas(checkout, out_dir)


main()
