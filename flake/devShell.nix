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
        ])
        ++ lib.attrValues config.treefmt.build.programs;
    };
  };
}
