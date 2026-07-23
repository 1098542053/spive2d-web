#!/usr/bin/env python3
"""E2E test with real Miku Live2D model - tests all major features."""
import json, urllib.request, sys, time, os

BASE = "http://localhost:3000"
PASS = 0
FAIL = 0

MODELS_DIR = r"I:\AICode\spive2d\spive2d-web\server-go\test_e2e_data\models"

def test(name, passed, detail=""):
    global PASS, FAIL
    if passed:
        print(f"  [PASS] {name}")
        PASS += 1
    else:
        print(f"  [FAIL] {name} - {detail}")
        FAIL += 1

class Session:
    def __init__(self):
        self.cookie = None
    def req(self, method, path, body=None):
        url = f"{BASE}{path}"
        data = json.dumps(body).encode() if body else None
        r = urllib.request.Request(url, data=data, method=method)
        r.add_header("Content-Type", "application/json")
        if self.cookie:
            r.add_header("Cookie", self.cookie)
        try:
            resp = urllib.request.urlopen(r)
            for h, v in resp.getheaders():
                if h.lower() == "set-cookie":
                    self.cookie = v.split(";")[0]
            return resp.status, json.loads(resp.read().decode())
        except urllib.error.HTTPError as e:
            try:
                return e.code, json.loads(e.read().decode())
            except:
                return e.code, {"error": str(e)}
        except Exception as e:
            return 0, {"error": str(e)}
    def raw(self, path):
        """Get raw bytes from a path (for images, files)."""
        url = f"{BASE}{path}"
        r = urllib.request.Request(url)
        if self.cookie:
            r.add_header("Cookie", self.cookie)
        try:
            resp = urllib.request.urlopen(r)
            return resp.status, resp.read()
        except urllib.error.HTTPError as e:
            return e.code, e.read()

s = Session()
uid = str(int(time.time() * 1000))[-6:]
print("=" * 70)
print("MIKU MODEL E2E TEST")
print("=" * 70)

# ──────────────────────────────────────────────
# 1. REGISTER
# ──────────────────────────────────────────────
print("\n--- 1. Register ---")
status, data = s.req("POST", "/api/auth/register",
    {"username": f"miku{uid}", "password": "testpass", "nickname": "MikuTester"})
test("register ok", status == 200, str(data.get("error","")))
test("session cookie set", s.cookie is not None)

# ──────────────────────────────────────────────
# 2. MODEL GALLERY - list
# ──────────────────────────────────────────────
print("\n--- 2. Model Gallery ---")
status, data = s.req("GET", "/api/models")
test("GET /api/models 200", status == 200, str(status))
test("has models key", "models" in data, str(list(data.keys())))

models = data.get("models", [])
test("at least 1 model found", len(models) >= 1,
     f"found {len(models)}: {[m['name'] for m in models]}")

if models:
    miku = models[0]
    print(f"\n  Model details: name={miku['name']}, type={miku['type']}, "
          f"scenes={miku['sceneCount']}, hasThumb={miku['hasThumb']}")
    test("model name = miku", miku["name"] == "miku",
         f"got {miku['name']}")
    test("model type = live2d", miku["type"] == "live2d",
         f"got {miku['type']}")
    test("scene count > 0", miku["sceneCount"] > 0,
         str(miku["sceneCount"]))
    test("has thumb path", miku.get("thumbPath", "").startswith("/api/thumbnail"),
         miku.get("thumbPath", ""))

# ──────────────────────────────────────────────
# 3. THUMBNAIL
# ──────────────────────────────────────────────
print("\n--- 3. Thumbnail ---")
if models:
    thumb_path = models[0].get("thumbPath", "")
    if thumb_path:
        status, img_data = s.raw(thumb_path)
        test("thumbnail status 200", status == 200, str(status))
        test("thumbnail has content", len(img_data) > 100,
             f"size={len(img_data)}")
        # Should be SVG placeholder (since no real thumbnail generated)
        test("thumbnail is svg or png",
             img_data[:6] in (b"<svg x", b"\x89PNG\r"),
             f"header={img_data[:20]}")

# ──────────────────────────────────────────────
# 4. FILE BROWSING
# ──────────────────────────────────────────────
print("\n--- 4. File Browsing ---")
# List files in miku model directory
model_path = os.path.join(MODELS_DIR, "miku").replace("\\", "/")
status, data = s.req("GET", f"/api/files?path={model_path}")
test("list model dir 200", status == 200, str(status))
test("has entries", "entries" in data, str(list(data.keys())))
entries = data.get("entries", [])
print(f"  Entries in miku/: {len(entries)}")
for e in entries:
    print(f"    {e['name']:30s} type={'dir' if e['isDir'] else 'file'}")

# Check that we can see subdirectories
dir_names = [e["name"] for e in entries if e["isDir"]]
test("has miku_free", "miku_free" in dir_names, str(dir_names))
test("has miku_pro", "miku_pro" in dir_names, str(dir_names))

# List files in runtime directory
runtime_path = os.path.join(MODELS_DIR, "miku", "miku_free", "runtime").replace("\\", "/")
status, data = s.req("GET", f"/api/files?path={runtime_path}")
test("list runtime dir 200", status == 200, str(status))
runtime_entries = data.get("entries", [])
moc3_files = [e["name"] for e in runtime_entries if e["name"].endswith(".moc3")]
json_files = [e["name"] for e in runtime_entries if e["name"].endswith(".json")]
png_files = [e["name"] for e in runtime_entries if e["name"].endswith(".png")]
test("runtime has miku.moc3", "miku.moc3" in moc3_files, str(moc3_files))
test("runtime has miku.model3.json", "miku.model3.json" in json_files, str(json_files))
# texture is in miku.2048/ subdirectory, check it exists recursively
has_texture = "texture_00.png" in png_files
if not has_texture:
    # Check subdirectories
    for e in runtime_entries:
        if e["isDir"]:
            sub_path = os.path.join(runtime_path, e["name"]).replace("\\", "/")
            _, sub_data = s.req("GET", f"/api/files?path={sub_path}")
            sub_files = [f["name"] for f in sub_data.get("entries", []) if f["name"] == "texture_00.png"]
            if sub_files:
                has_texture = True
                break
test("runtime has texture_00.png", has_texture, f"in runtime/ or subdirs: {png_files}")

# ──────────────────────────────────────────────
# 5. FILE CONTENT ACCESS (model3.json)
# ──────────────────────────────────────────────
print("\n--- 5. Model JSON Content ---")
json_path = os.path.join(MODELS_DIR, "miku", "miku_free", "runtime", "miku.model3.json").replace("\\", "/")
status, raw = s.raw(f"/api/file?path={json_path}")
test("read model3.json 200", status == 200, str(status))
if status == 200:
    model_json = json.loads(raw.decode("utf-8"))
    test("model3.json has Version", "Version" in model_json, str(list(model_json.keys())[:5]))
    test("model3.json has FileReferences", "FileReferences" in model_json)

# ──────────────────────────────────────────────
# 6. FILES API UNIFIED RESPONSE
# ──────────────────────────────────────────────
print("\n--- 6. Files API Unified Response ---")
status, data = s.req("GET", f"/api/files?path={model_path}")
test("has items key", "items" in data)
test("has entries key", "entries" in data)
test("has files key", "files" in data)
test("items is list (not null)", isinstance(data["items"], list),
     f"type={type(data['items']).__name__}")

# ──────────────────────────────────────────────
# 7. SETTINGS - change modelsRoot
# ──────────────────────────────────────────────
print("\n--- 7. Settings: Change modelsRoot ---")
empty_dir = os.path.join(MODELS_DIR, "..", "empty_models").replace("\\", "/")
os.makedirs(empty_dir, exist_ok=True)

status, data = s.req("POST", "/api/settings", {"modelsRoot": empty_dir})
test("settings save 200", status == 200, str(data))

status, data = s.req("GET", "/api/settings")
test("settings read back", data.get("modelsRoot") == empty_dir,
     f"got {data.get('modelsRoot')}")

# ──────────────────────────────────────────────
# 8. GALLERY AFTER CONFIG CHANGE
# ──────────────────────────────────────────────
print("\n--- 8. Gallery after modelsRoot change ---")
status, data = s.req("GET", "/api/models")
test("GET /api/models 200", status == 200, str(status))
models_after = data.get("models", [])
test("empty dir = 0 models", len(models_after) == 0,
     f"got {len(models_after)} models (should be 0 for empty dir)")

# Restore original modelsRoot
status, data = s.req("POST", "/api/settings", {"modelsRoot": MODELS_DIR})
test("restore modelsRoot", status == 200, str(data))

# ──────────────────────────────────────────────
# 9. DIAGNOSE
# ──────────────────────────────────────────────
print("\n--- 9. Diagnose ---")
status, data = s.req("GET", "/api/diagnose")
test("diagnose 200", status == 200, str(status))
test("diagnose has status", "status" in data)
test("diagnose has modelsRootExists", "modelsRootExists" in data)
test("diagnose has modelCount", "modelCount" in data)
print(f"  Diagnose status={data.get('status')}, "
      f"modelsRootExists={data.get('modelsRootExists')}, "
      f"modelCount={data.get('modelCount')}")

# ──────────────────────────────────────────────
# SUMMARY
# ──────────────────────────────────────────────
print(f"\n{'='*70}")
print(f"FINAL: {PASS} PASS, {FAIL} FAIL")
print(f"{'='*70}")
sys.exit(FAIL)
