{ system, inputJSON, catalogJSON }:
{
  result =
  let
    input = builtins.fromJSON inputJSON;
    source = builtins.getFlake input.flakeRef;
    pkgs = import source.outPath {
      inherit system;
      overlays = [];
      # Evaluate advertised unfree targets without installing or weakening validity checks.
      config.allowUnfree = true;
    };
    evaluate = entry:
      let
        pkg = pkgs.lib.attrByPath
          (pkgs.lib.splitString "." entry.nix.attribute)
          (throw "missing catalog attribute: ${entry.nix.attribute}")
          pkgs;
        identity = { inherit (entry) id; attribute = entry.nix.attribute; };
      in
      if pkgs.lib.meta.availableOn pkgs.stdenv.hostPlatform pkg then
        identity // { status = "available"; drvPath = pkg.drvPath; }
      else
        identity // { status = "unavailable"; reason = "declared-platform"; };
  in
  assert source.rev == input.revision;
  assert source.narHash == input.narHash;
  {
    schemaVersion = 1;
    status = "passed";
    inherit system;
    nixpkgs = { inherit (input) revision flakeRef narHash; };
    # JSON evaluation forces available drvPaths. Errors never become unavailable rows.
    mappings = map evaluate (builtins.fromJSON catalogJSON);
  };
}
