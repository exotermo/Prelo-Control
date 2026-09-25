package dev.hermes.bridge.service;

import dev.hermes.bridge.config.BridgeProperties;
import java.util.concurrent.atomic.AtomicBoolean;
import org.springframework.stereotype.Component;

// Two switches in series: deployment configuration is the fail-closed safety baseline, while
// the in-memory runtime switch lets an operator stop/resume automation without a redeploy.
@Component
public class AutoReplyGate {
    private final BridgeProperties properties;
    private final AtomicBoolean runtimeEnabled = new AtomicBoolean(true);

    public AutoReplyGate(BridgeProperties properties) {
        this.properties = properties;
    }

    public boolean isEnabled() {
        return properties.autoReplyEnabled() && runtimeEnabled.get();
    }

    public void pause() { runtimeEnabled.set(false); }

    public void resume() { runtimeEnabled.set(true); }

    public boolean isRuntimeEnabled() { return runtimeEnabled.get(); }
}
