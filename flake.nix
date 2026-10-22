{
  description = "MCP server for Jellyfin";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };

    systems.url = "github:nix-systems/default";
  };

  outputs =
    inputs@{
      self,
      flake-parts,
      systems,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = import systems;

      perSystem =
        { pkgs, self', ... }:
        let
          rev = self.shortRev or self.dirtyShortRev or "dev";
          version = "0.1.0";
        in
        {
          packages.default = pkgs.buildGoModule {
            pname = "jellyfin-mcp";
            inherit version;
            src = pkgs.lib.fileset.toSource {
              root = ./.;
              fileset = pkgs.lib.fileset.unions [
                ./go.mod
                ./go.sum
                ./cmd
                ./internal
              ];
            };
            ldflags = [
              "-s"
              "-X main.version=${version}+${rev}"
            ];
            # Update after changing go.mod: set to pkgs.lib.fakeHash, run `nix build`, copy the printed hash.
            vendorHash = "sha256-NbuvbsmNbz3aKdjyyfpkbi52yRhXl473j3LvAUDn3L8=";
            env.CGO_ENABLED = 0;
            meta = {
              description = "MCP server for Jellyfin";
              homepage = "https://github.com/jardarton/jellyfin-mcp";
              license = pkgs.lib.licenses.mit;
              mainProgram = "jellyfin-mcp";
            };
          };

          # OCI image serving MCP over HTTP on :8080. `nix build .#image && docker load < result`
          packages.image = pkgs.dockerTools.buildLayeredImage {
            name = "ghcr.io/jardarton/jellyfin-mcp";
            tag = rev;
            contents = [ pkgs.cacert ];
            config = {
              Entrypoint = [ (pkgs.lib.getExe self'.packages.default) ];
              User = "65534:65534";
              ExposedPorts."8080/tcp" = { };
              Env = [
                "JELLYFIN_MCP_TRANSPORT=http"
                "JELLYFIN_MCP_ADDR=:8080"
                "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt"
              ];
              Labels = {
                # Links the ghcr package to the repo, so it inherits the repo's visibility.
                "org.opencontainers.image.source" = "https://github.com/jardarton/jellyfin-mcp";
                "org.opencontainers.image.description" = "MCP server for Jellyfin";
                "org.opencontainers.image.licenses" = "MIT";
                "org.opencontainers.image.revision" = self.rev or self.dirtyRev or "dev";
                "org.opencontainers.image.version" = version;
              };
            };
          };

          checks = {
            # Building the package runs the Go tests.
            default = self'.packages.default;
            gofmt =
              pkgs.runCommand "gofmt"
                {
                  nativeBuildInputs = [ pkgs.go ];
                  src = self'.packages.default.src;
                }
                ''
                  unformatted=$(gofmt -l "$src")
                  if [ -n "$unformatted" ]; then
                    echo "not gofmt'd:" $unformatted
                    exit 1
                  fi
                  touch $out
                '';
          };

          devShells.default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gopls
              gotools
              nixfmt
              skopeo
            ];
          };

          formatter = pkgs.nixfmt;
        };
    };
}
