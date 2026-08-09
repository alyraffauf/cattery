{
  description = "Cattery — a safe, cross-platform dotfiles manager";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/ee48b147c18c7de1e6ec97dc74792be42724bed1";

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = function:
        nixpkgs.lib.genAttrs supportedSystems (system: function system);
      pkgsFor = system: import nixpkgs { inherit system; };
      shellPackages = pkgs: with pkgs; [
        go_1_26
        go_1_25
        just
        go-tools
        sops
        age
        python3
        shellcheck
        gh
        gnutar
        gzip
      ];
    in
    {
      devShells = forAllSystems (system:
        let pkgs = pkgsFor system; in
        {
          default = pkgs.mkShell {
            packages = shellPackages pkgs;
          };
        });

      checks = forAllSystems (system: {
        devShell = self.devShells.${system}.default;
      });
    };
}
