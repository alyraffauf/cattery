_: {
  perSystem = {
    config,
    lib,
    pkgs,
    ...
  }: {
    devShells.default = pkgs.mkShell {
      packages =
        (with pkgs; [
          go_1_26
          just
          sops
          age
        ])
        ++ lib.attrValues config.treefmt.build.programs;
    };
  };
}
