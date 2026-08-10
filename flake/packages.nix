_: {
  perSystem = {pkgs, ...}: let
    cattery = pkgs.callPackage ../nix/cattery.nix {};
  in {
    packages = {
      inherit cattery;
      default = cattery;
    };
  };
}
