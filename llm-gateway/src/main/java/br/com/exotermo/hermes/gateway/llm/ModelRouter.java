package br.com.exotermo.hermes.gateway.llm;

import java.util.List;
import org.springframework.stereotype.Component;

@Component
public class ModelRouter {
    private final List<LLMProvider> providers;
    public ModelRouter(List<LLMProvider> providers) { this.providers = List.copyOf(providers); }
    public LLMProvider route(String model) {
        return providers.stream().filter(provider -> provider.supports(model)).findFirst()
            .orElseThrow(() -> new UnsupportedModelException(model));
    }
}
