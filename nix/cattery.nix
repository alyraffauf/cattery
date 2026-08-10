{
  buildGoModule,
  lib,
}:
buildGoModule {
  pname = "cattery";
  version = "dev";
  src = ../.;
  proxyVendor = true;
  vendorHash = "sha256-UIGv/yN4FHE9smAgvsjDpnKyPXbjewPs5x4q2aOiFlE=";
  subPackages = ["cmd/cattery"];

  # The unit and integration suites shell out to `go build` and run real
  # subprocesses, which doesn't fit the hermetic build sandbox. CI runs
  # `go test ./...` against a full Go toolchain instead.
  doCheck = false;

  ldflags = [
    "-s"
    "-w"
  ];

  meta = with lib; {
    description = "Safe, cross-platform dotfiles manager";
    homepage = "https://github.com/alyraffauf/cattery";
    license = licenses.mit;
    platforms = platforms.unix;
    mainProgram = "cattery";
  };
}
