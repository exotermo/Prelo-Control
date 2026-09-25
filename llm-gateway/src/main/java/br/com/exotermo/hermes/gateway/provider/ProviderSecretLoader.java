package br.com.exotermo.hermes.gateway.provider;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import org.springframework.stereotype.Component;

// Reads provider credentials exclusively from a mounted file — never from an environment
// variable. Fails fast at bean-construction time (application startup) when the enabled
// provider's secret file is missing or empty, instead of surfacing as a runtime error on the
// first request.
@Component
public class ProviderSecretLoader {
    public String load(String path) {
        if (path == null || path.isBlank()) throw new IllegalStateException("provider secret file path is not configured");
        String content;
        try {
            content = Files.readString(Path.of(path)).strip();
        } catch (IOException exception) {
            throw new IllegalStateException("unable to read provider secret file: " + path, exception);
        }
        if (content.isEmpty()) throw new IllegalStateException("provider secret file is empty: " + path);
        return content;
    }

    @Override public String toString() { return "ProviderSecretLoader{}"; }
}
