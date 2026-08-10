_: {
  perSystem = _: {
    treefmt.config = {
      programs = {
        alejandra.enable = true;
        deadnix.enable = true;
        gofmt.enable = true;
        statix.enable = true;
      };
    };
  };
}
