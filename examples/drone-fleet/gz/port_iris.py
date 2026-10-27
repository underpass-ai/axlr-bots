#!/usr/bin/env python3
"""Make port_iris: the plugin's iris_with_standoffs with simpler collisions.

    port_iris.py SRC_MODELS DST_MODELS

Copies SRC_MODELS/iris_with_standoffs to DST_MODELS/port_iris (the model
keeps its name, so the quad template's iris_with_standoffs:: links stay)
and changes what it touches the ground with: the body's 2,884-triangle mesh
becomes the box around it, each leg's thin cylinder a sphere at its foot.
A resting cylinder gives the contact solver several points per leg; six
quads on their pads then ran at 0.27 x real time, with spheres at 1.00.
The visuals still come from model://iris_with_standoffs.
"""

import os
import re
import shutil
import sys

BODY = (r"(<collision name=.base_collision.>\s*)<pose>0 0 0 0 0 0</pose>(\s*<geometry>\s*)"
        r"<mesh>\s*<uri>model://iris_with_standoffs/meshes/iris_collision.stl</uri>\s*</mesh>")
# iris_collision.stl spans x -0.160..0.145, y -0.235..0.239, z -0.051..0.039.
BOX = r"\1<pose>-0.0075 0.002 -0.006 0 0 0</pose>\2<box><size>0.305 0.474 0.09</size></box>"
LEG = (r"(<collision name=.(?:front|rear)_(?:left|right)_leg_collision.>\s*)<pose>(\S+) (\S+) -0.11 0 0 0</pose>"
       r"(\s*<geometry>\s*)<cylinder>\s*<radius>0.005</radius>\s*<length>0.17</length>\s*</cylinder>")
# The leg's foot is at -0.11 - 0.17 / 2 = -0.195: the sphere's bottom stays there.
FOOT = r"\1<pose>\2 \3 -0.185 0 0 0</pose>\4<sphere><radius>0.01</radius></sphere>"


def simplify(sdf):
    sdf, body = re.subn(BODY, BOX, sdf)
    sdf, legs = re.subn(LEG, FOOT, sdf)
    if (body, legs) != (1, 4):
        raise ValueError(f"iris_with_standoffs has changed: {body} body and {legs} leg collisions matched")
    return sdf


def main():
    src, dst = (os.path.join(d, m) for d, m in zip(sys.argv[1:3], ("iris_with_standoffs", "port_iris")))
    shutil.rmtree(dst, ignore_errors=True)
    os.makedirs(dst)
    with open(os.path.join(src, "model.sdf")) as f:
        sdf = simplify(f.read())
    with open(os.path.join(dst, "model.sdf"), "w") as f:
        f.write(sdf)
    with open(os.path.join(src, "model.config")) as f:
        config = re.sub(r"<name>[^<]*</name>", "<name>port_iris</name>", f.read(), count=1)
    with open(os.path.join(dst, "model.config"), "w") as f:
        f.write(config)
    print(f"{dst}: box body, sphere feet")


if __name__ == "__main__":
    main()
