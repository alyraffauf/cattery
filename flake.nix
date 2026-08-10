{
  description = "Cattery — a safe, cross-platform dotfiles manager";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" ];
      forAllSystems = function:
        nixpkgs.lib.genAttrs supportedSystems (system: function system);
      pkgsFor = system: import nixpkgs { inherit system; };
      shellPackages = pkgs: with pkgs; [
        go_1_26
        just
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
