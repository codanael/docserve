{
  description = "Podstrim — open-source remote podcast recording platform";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    claude-code.url = "github:sadjow/claude-code-nix";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
    claude-code,
  }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ claude-code.overlays.default ];
          config.allowUnfreePredicate = pkg: builtins.elem (nixpkgs.lib.getName pkg) [
            "claude-code"
          ];
        };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.claude-code
            # Go
            pkgs.go_1_26
            pkgs.gopls
            pkgs.gotools
            pkgs.golangci-lint
            pkgs.goreleaser

            # Node.js / Frontend
            pkgs.nodejs_24

            # Base de donnees
            pkgs.postgresql_18

            # Docker
            pkgs.docker-compose

            # Audio
            pkgs.sox

            # E2E testing
            pkgs.playwright-driver
            pkgs.playwright-driver.browsers

            # Security
            pkgs.osv-scanner

            # Outils systeme
            pkgs.openssl
            pkgs.git
            pkgs.jq
            pkgs.curl
          ];

          shellHook = ''
            echo "Podstrim dev environment loaded"
            echo "Go:   $(go version)"
            echo "Node: $(node --version)"
            echo "npm:  $(npm --version)"

            # Playwright: use nix-provided browsers (NixOS can't run downloaded binaries)
            export PLAYWRIGHT_BROWSERS_PATH="${pkgs.playwright-driver.browsers}"
            export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
            export PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=true

            # Installer les deps Node si necessaire
            if [ -f "frontend/package.json" ] && [ ! -d "frontend/node_modules" ]; then
              echo "Installing Node dependencies..."
              (cd frontend && npm install)
            fi
          '';
        };
      }
    );
}
