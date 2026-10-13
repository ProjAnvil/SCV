package projanvil.scv.beanprobe;

import com.google.gson.GsonBuilder;
import spoon.Launcher;
import spoon.MavenLauncher;
import spoon.reflect.CtModel;
import spoon.reflect.declaration.*;
import spoon.reflect.reference.CtTypeReference;
import spoon.reflect.visitor.Filter;

import java.io.File;
import java.io.InputStream;
import java.io.PrintWriter;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.*;

/**
 * BeanProbe — static Spring bean dependency-graph extractor for SCV.
 *
 * Extracts bean definitions and injection edges from Spring Boot source code
 * (Maven or Gradle builds) and emits a normalized JSON graph in Vectra batch
 * format: nodes {key,label,props}, edges {srcKey,dstKey,label:"INJECTS",props}.
 *
 * Provenance: derived from the October 2026 feasibility spike; validated against
 * spring-petclinic and the fixtures in beanprobe/fixtures (see README).
 *
 * Usage: BeanProbe <maven|gradle|noclasspath> <projectDir> <outJson>
 */
public class BeanProbe {

    static final Set<String> STEREOTYPES = new LinkedHashSet<>(List.of(
            "org.springframework.stereotype.Component",
            "org.springframework.stereotype.Service",
            "org.springframework.stereotype.Repository",
            "org.springframework.stereotype.Controller",
            "org.springframework.web.bind.annotation.RestController",
            "org.springframework.web.bind.annotation.ControllerAdvice",
            "org.springframework.web.bind.annotation.RestControllerAdvice",
            "org.springframework.context.annotation.Configuration"
    ));
    static final String AUTOWIRED = "org.springframework.beans.factory.annotation.Autowired";
    static final String QUALIFIER = "org.springframework.beans.factory.annotation.Qualifier";
    static final String BEAN_METHOD = "org.springframework.context.annotation.Bean";
    static final String PRIMARY = "org.springframework.context.annotation.Primary";
    static final String VALUE_ANNO = "org.springframework.beans.factory.annotation.Value";
    static final String DATA_REPOSITORY = "org.springframework.data.repository.Repository";
    static final String LOMBOK_RCA = "lombok.RequiredArgsConstructor";
    static final String LOMBOK_NONNULL = "lombok.NonNull";
    static final String ENABLE_CONFIG_PROPS = "org.springframework.boot.context.properties.EnableConfigurationProperties";
    static final Set<String> WRAPPER_TYPES = Set.of(
            "java.util.List", "java.util.Set", "java.util.Collection", "java.lang.Iterable",
            "java.util.Optional", "org.springframework.beans.factory.ObjectProvider");

    static class BeanDef {
        String beanName;
        String typeQfn;
        String stereotype;
        List<String> conditions = new ArrayList<>();
        String file; int line;
        CtTypeReference<?> typeRef;
        boolean isPrimary;
    }

    static class InjectionPoint {
        CtTypeReference<?> type;
        String name;            // field or parameter simple name (for by-name disambiguation)
        String qualifier;
        String via;            // constructor | lombok-constructor | field | inherited-field | setter | beanMethod
        String wraps;          // List/Set/Optional/ObjectProvider when unwrapped, else null
        boolean collection;    // true => multi-candidate is BY DESIGN, never ambiguous
        String at;
    }

    record Edge(String srcKey, String dstKey, String via, String qualifier, boolean ambiguous, String wraps, String at) {}

    static final List<String> unresolved = new ArrayList<>();

    public static void main(String[] args) throws Exception {
        if (args.length < 3) {
            System.err.println("usage: BeanProbe <maven|gradle|noclasspath> <projectDir> <outJson>");
            System.exit(2);
        }
        String mode = args[0];
        File projectDir = new File(args[1]).getAbsoluteFile();
        File outJson = new File(args[2]).getAbsoluteFile();
        outJson.getParentFile().mkdirs();

        long t0 = System.currentTimeMillis();
        Launcher launcher = switch (mode) {
            case "maven" -> new MavenLauncher(projectDir.getAbsolutePath(), MavenLauncher.SOURCE_TYPE.APP_SOURCE);
            case "gradle", "noclasspath" -> buildLauncherWithClasspath(mode, projectDir);
            default -> throw new IllegalArgumentException("mode " + mode);
        };
        launcher.getEnvironment().setComplianceLevel(21);
        CtModel model = launcher.buildModel();
        System.out.println("[model] built in " + (System.currentTimeMillis() - t0) + " ms");

        List<CtType<?>> allTypes = model.getElements((Filter<CtType<?>>) t ->
                t.getSimpleName() != null && !t.getSimpleName().isEmpty());
        System.out.println("[model] declared types: " + allTypes.size());

        // phase 1: collect beans
        Map<String, BeanDef> beans = new LinkedHashMap<>();
        Set<String> beanProducedTypes = new HashSet<>();   // return types of @Bean methods
        for (CtType<?> t : allTypes) {
            if (t instanceof CtAnnotationType<?>) continue;   // annotation types are not beans
            String stereo = detectStereotype(t);
            boolean dataRepo = false;
            if (stereo == null && t.isInterface() && isSubtypeQfn(t.getReference(), DATA_REPOSITORY)) {
                stereo = "data-repository";
                dataRepo = true;
            }
            if (stereo == null) continue;
            BeanDef def = new BeanDef();
            def.stereotype = dataRepo ? "data-repository" : stereo.substring(stereo.lastIndexOf('.') + 1);
            def.beanName = explicitName(t) != null ? explicitName(t) : decapitalize(t.getSimpleName());
            def.typeQfn = t.getQualifiedName();
            def.typeRef = t.getReference();
            def.file = posFile(t); def.line = posLine(t);
            def.conditions = collectConditions(t);
            def.isPrimary = hasAnnotationQfn(t, PRIMARY);
            beans.put("bean:" + def.beanName, def);
            if (!dataRepo) {
                for (CtMethod<?> m : t.getMethods()) {
                    if (!hasAnnotationQfn(m, BEAN_METHOD)) continue;
                    BeanDef bd = new BeanDef();
                    bd.beanName = m.getSimpleName();
                    bd.stereotype = "@Bean";
                    bd.typeQfn = qfn(m.getType());
                    bd.typeRef = m.getType();
                    bd.file = posFile(m); bd.line = posLine(m);
                    bd.conditions = collectConditions(m);
                    bd.isPrimary = hasAnnotationQfn(m, PRIMARY);
                    beans.put("bean:" + bd.beanName, bd);
                    beanProducedTypes.add(bd.typeQfn);
                }
            }
        }

        // @EnableConfigurationProperties(X.class) registrations.
        // NOTE: getValueAsObject would try to LOAD the referenced class (classloader explosion
        // for sibling-module source types), so we regex the expression string and match
        // against the model instead.
        Map<String, CtType<?>> typesByQfn = new HashMap<>();
        for (CtType<?> t : allTypes) typesByQfn.put(t.getQualifiedName(), t);
        for (CtType<?> t : allTypes) {
            for (CtAnnotation<?> a : t.getAnnotations()) {
                if (!ENABLE_CONFIG_PROPS.equals(a.getAnnotationType().getQualifiedName())) continue;
                String raw = a.getValue("value") == null ? "" : a.getValue("value").toString();
                java.util.regex.Matcher m = java.util.regex.Pattern.compile("([\\w.$]+)\\.class").matcher(raw);
                while (m.find()) {
                    String qfn = m.group(1);
                    boolean already = beans.values().stream().anyMatch(b -> b.typeQfn.equals(qfn));
                    if (already) continue;
                    CtType<?> dt = typesByQfn.get(qfn);
                    if (dt == null) continue;
                    BeanDef bd = new BeanDef();
                    bd.beanName = decapitalize(dt.getSimpleName());
                    bd.stereotype = "ConfigurationProperties";
                    bd.typeQfn = qfn;
                    bd.typeRef = dt.getReference();
                    bd.file = posFile(t); bd.line = posLine(t);
                    bd.conditions = List.of("EnableConfigurationProperties(" + t.getSimpleName() + ")");
                    beans.put("bean:" + bd.beanName, bd);
                }
            }
        }

        // phase 2: DI edges
        Map<String, List<InjectionPoint>> injectionsByBean = new LinkedHashMap<>();
        for (CtType<?> t : allTypes) {
            String ownerKey = ownerBeanKey(t, beans);
            boolean beanProduced = beanProducedTypes.contains(t.getQualifiedName()); // @Bean method params already cover it
            List<InjectionPoint> ips = (ownerKey != null && !beanProduced) ? classInjectionPoints(t) : List.of();
            if (ownerKey != null) injectionsByBean.computeIfAbsent(ownerKey, k -> new ArrayList<>()).addAll(ips);
            if (ownerKey == null) continue;
            for (CtMethod<?> m : t.getMethods()) {
                if (!hasAnnotationQfn(m, BEAN_METHOD)) continue;
                String beanKey = beans.containsKey("bean:" + m.getSimpleName()) ? "bean:" + m.getSimpleName() : ownerKey;
                for (CtParameter<?> p : m.getParameters()) {
                    if (hasAnnotationQfn(p, VALUE_ANNO)) continue;
                    InjectionPoint ip = basePoint(p.getType(), p.getSimpleName(), "beanMethod", p);
                    injectionsByBean.computeIfAbsent(beanKey, k -> new ArrayList<>()).add(ip);
                }
            }
        }

        List<Edge> edges = new ArrayList<>();
        Set<String> edgeKeys = new HashSet<>();
        for (Map.Entry<String, List<InjectionPoint>> e : injectionsByBean.entrySet()) {
            for (InjectionPoint ip : e.getValue()) {
                List<BeanDef> candidates = resolveCandidates(ip, beans);
                if (candidates.isEmpty()) {
                    boolean frameworkNoise = ip.type == null || ip.type.getQualifiedName() == null
                            || ip.type.getQualifiedName().startsWith("java.") || ip.type.getQualifiedName().startsWith("jdk.");
                    if (!frameworkNoise) {
                        unresolved.add(e.getKey() + " needs " + qfn(ip.type) + (ip.qualifier != null ? " @" + ip.qualifier : "")
                                + " (" + ip.via + (ip.wraps != null ? "<" + ip.wraps + ">" : "") + " @ " + ip.at + ")");
                    }
                    continue;
                }
                boolean ambiguous = !ip.collection && candidates.size() > 1;
                for (BeanDef c : candidates) {
                    if (("bean:" + c.beanName).equals(e.getKey())) continue;
                    String ek = e.getKey() + "->bean:" + c.beanName + ":" + ip.via + ":" + ip.name;
                    if (!edgeKeys.add(ek)) continue;
                    edges.add(new Edge(e.getKey(), "bean:" + c.beanName, ip.via, ip.qualifier, ambiguous, ip.wraps, ip.at));
                }
            }
        }

        // output: Vectra batch JSON
        Map<String, Object> out = new LinkedHashMap<>();
        List<Map<String, Object>> nodes = new ArrayList<>();
        for (Map.Entry<String, BeanDef> e : beans.entrySet()) {
            BeanDef b = e.getValue();
            Map<String, Object> n = new LinkedHashMap<>();
            n.put("key", e.getKey());
            n.put("label", "Bean");
            Map<String, Object> props = new LinkedHashMap<>();
            props.put("beanName", b.beanName);
            props.put("type", b.typeQfn);
            props.put("stereotype", b.stereotype);
            props.put("primary", b.isPrimary);
            if (!b.conditions.isEmpty()) props.put("conditions", String.join("; ", b.conditions));
            props.put("source", b.file + ":" + b.line);
            n.put("props", props);
            nodes.add(n);
        }
        List<Map<String, Object>> edgeJson = new ArrayList<>();
        for (Edge ed : edges) {
            Map<String, Object> j = new LinkedHashMap<>();
            j.put("srcKey", ed.srcKey());
            j.put("dstKey", ed.dstKey());
            j.put("label", "INJECTS");
            Map<String, Object> props = new LinkedHashMap<>();
            props.put("via", ed.via());
            if (ed.qualifier() != null) props.put("qualifier", ed.qualifier());
            if (ed.wraps() != null) props.put("wraps", ed.wraps());
            if (ed.ambiguous()) props.put("ambiguous", true);
            props.put("at", ed.at());
            j.put("props", props);
            edgeJson.add(j);
        }
        out.put("nodes", nodes);
        out.put("edges", edgeJson);
        try (PrintWriter w = new PrintWriter(outJson)) {
            w.println(new GsonBuilder().setPrettyPrinting().create().toJson(out));
        }

        Map<String, Integer> byStereo = new TreeMap<>();
        beans.values().forEach(b -> byStereo.merge(b.stereotype, 1, Integer::sum));
        System.out.println("\n=== BEANPROBE RESULT ===");
        System.out.println("beans: " + beans.size() + "  by stereotype: " + byStereo);
        System.out.println("INJECTS edges: " + edges.size() + "  (ambiguous: "
                + edges.stream().filter(Edge::ambiguous).count() + ")");
        System.out.println("unresolved injections: " + unresolved.size());
        unresolved.forEach(u -> System.out.println("  UNRESOLVED " + u));
        System.out.println("json written: " + outJson);
    }

    // ---------- launcher / classpath ----------
    static Launcher buildLauncherWithClasspath(String mode, File projectDir) throws Exception {
        Launcher l = new Launcher();
        List<File> sourceRoots = new ArrayList<>();
        if (mode.equals("gradle")) {
            Map<String, String[]> cps = runGradleClasspath(projectDir);
            LinkedHashSet<String> entries = new LinkedHashSet<>();
            for (Map.Entry<String, String[]> e : cps.entrySet()) {
                System.out.println("[gradle] project " + e.getKey() + " classpath entries: " + e.getValue().length);
                entries.addAll(List.of(e.getValue()));
                String rel = e.getKey().replace(':', File.separatorChar);
                File src = new File(projectDir, rel + "/src/main/java");
                if (src.isDirectory()) sourceRoots.add(src);
            }
            List<String> existing = new ArrayList<>();
            int dropped = 0;
            for (String e : entries) {
                if (new File(e).exists()) existing.add(e); else dropped++;
            }
            System.out.println("[gradle] classpath: " + existing.size() + " kept, " + dropped + " dropped (unbuilt sibling jars)");
            l.getEnvironment().setSourceClasspath(existing.toArray(new String[0]));
        } else {
            l.getEnvironment().setNoClasspath(true);
            System.out.println("[noclasspath] degraded mode");
            File src = new File(projectDir, "src/main/java");
            if (src.isDirectory()) sourceRoots.add(src);
        }
        if (sourceRoots.isEmpty()) sourceRoots.add(projectDir);
        sourceRoots.forEach(r -> l.addInputResource(r.getAbsolutePath()));
        System.out.println("[gradle] source roots: " + sourceRoots);
        return l;
    }

    static Map<String, String[]> runGradleClasspath(File projectDir) throws Exception {
        File initScript = materializeInitScript();
        String gradle = new File(projectDir, "gradlew").canExecute() ? "./gradlew" : "gradle";
        List<String> cmd = new ArrayList<>(List.of(gradle, "-q", "--console=plain", "--no-configuration-cache",
                "spoonPrintClasspath", "--init-script", initScript.getAbsolutePath()));
        ProcessBuilder pb = new ProcessBuilder(cmd);
        pb.directory(projectDir);
        pb.redirectErrorStream(true);
        Process p = pb.start();
        Map<String, LinkedHashSet<String>> byProject = new LinkedHashMap<>();
        try (Scanner sc = new Scanner(p.getInputStream(), "UTF-8")) {
            while (sc.hasNextLine()) {
                String line = sc.nextLine();
                if (line.startsWith("SPOONCP::")) {
                    String rest = line.substring("SPOONCP::".length());
                    int close = rest.indexOf("]::");
                    String projPath = close >= 0 ? rest.substring(0, close).replace("[", "").replace("]", "") : ":";
                    String cp = close >= 0 ? rest.substring(close + 3) : rest;
                    byProject.computeIfAbsent(projPath, k -> new LinkedHashSet<>())
                           .addAll(List.of(cp.split(File.pathSeparator)));
                } else {
                    System.out.println("[gradle] " + line);
                }
            }
        }
        int code = p.waitFor();
        if (code != 0) throw new IllegalStateException("gradle exited " + code);
        Map<String, String[]> out = new LinkedHashMap<>();
        byProject.forEach((k, v) -> {
            Set<String> clean = new LinkedHashSet<>();
            v.forEach(e -> { if (!e.isBlank()) clean.add(e); });
            out.put(k, clean.toArray(new String[0]));
        });
        return out;
    }

    /** The Gradle init script ships as a classpath resource so the tool is a single artifact. */
    static File materializeInitScript() throws Exception {
        try (InputStream in = BeanProbe.class.getResourceAsStream("/cp-init.gradle")) {
            if (in == null) throw new IllegalStateException("cp-init.gradle not on classpath");
            File f = File.createTempFile("beanprobe-cp-init", ".gradle");
            f.deleteOnExit();
            Files.copy(in, f.toPath(), StandardCopyOption.REPLACE_EXISTING);
            return f;
        }
    }

    // ---------- bean detection ----------
    static String detectStereotype(CtType<?> t) {
        for (CtAnnotation<?> a : t.getAnnotations()) {
            String q = a.getAnnotationType().getQualifiedName();
            if (STEREOTYPES.contains(q)) return q;
        }
        // meta-annotation walk: handles @Service, custom stereotypes, @SpringBootApplication
        for (CtAnnotation<?> a : t.getAnnotations()) {
            String hit = walkMeta(a.getAnnotationType(), new HashSet<>(), 0);
            if (hit != null) return hit;
        }
        return null;
    }

    static String walkMeta(CtTypeReference<?> annoRef, Set<String> visited, int depth) {
        if (annoRef == null || depth > 6) return null;
        String q = annoRef.getQualifiedName();
        if (!visited.add(q)) return null;
        if (STEREOTYPES.contains(q)) return q;
        CtType<?> decl = annoRef.getTypeDeclaration();   // shadow type if from classpath jar
        if (decl == null) return null;
        for (CtAnnotation<?> a : decl.getAnnotations()) {
            String hit = walkMeta(a.getAnnotationType(), visited, depth + 1);
            if (hit != null) return hit;
        }
        return null;
    }

    static String explicitName(CtType<?> t) {
        for (CtAnnotation<?> a : t.getAnnotations()) {
            if (STEREOTYPES.contains(a.getAnnotationType().getQualifiedName())) {
                String v = annoValue(a, "value");
                if (v != null && !v.isEmpty()) return v;
            }
        }
        return null;
    }

    static List<String> collectConditions(CtElement el) {
        List<String> out = new ArrayList<>();
        for (CtAnnotation<?> a : el.getAnnotations()) {
            String q = a.getAnnotationType().getQualifiedName();
            if (q.startsWith("org.springframework.boot.autoconfigure.condition.ConditionalOn")
                    || q.equals("org.springframework.context.annotation.Profile")) {
                String simple = q.substring(q.lastIndexOf('.') + 1);
                String name = annoValue(a, "name");
                String value = annoValue(a, "value");
                String hv = annoValue(a, "havingValue");
                String detail = name != null ? name : value;
                out.add(detail != null ? simple + "(" + detail + (hv != null ? "=" + hv : "") + ")" : simple);
            }
        }
        return out;
    }

    // ---------- injection point extraction ----------
    static InjectionPoint basePoint(CtTypeReference<?> type, String name, String via, CtElement where) {
        InjectionPoint ip = new InjectionPoint();
        ip.name = name;
        ip.via = via;
        ip.at = posFile(where) + ":" + posLine(where);
        CtTypeReference<?> unwrapped = unwrap(type);
        if (unwrapped != type) {
            ip.wraps = type.getQualifiedName().substring(type.getQualifiedName().lastIndexOf('.') + 1);
            ip.collection = WRAPPER_TYPES.stream().anyMatch(w -> (type.getQualifiedName().equals(w)
                    && !w.equals("java.util.Optional") && !w.endsWith("ObjectProvider")));
            ip.type = unwrapped;
        } else {
            ip.type = type;
        }
        ip.qualifier = where instanceof CtParameter<?> p ? paramQualifier(p)
                : where instanceof CtField<?> f ? fieldQualifier(f) : null;
        return ip;
    }

    static CtTypeReference<?> unwrap(CtTypeReference<?> type) {
        String q = type == null ? null : type.getQualifiedName();
        if (q == null || !WRAPPER_TYPES.contains(q)) return type;
        List<CtTypeReference<?>> args = type.getActualTypeArguments();
        if (args == null || args.size() != 1) return type;
        CtTypeReference<?> elem = args.get(0);
        return elem == null ? type : elem;
    }

    static List<InjectionPoint> classInjectionPoints(CtType<?> t) {
        List<InjectionPoint> ips = new ArrayList<>();

        // constructors: @Autowired-annotated wins; else exactly-one non-default; else skip (log)
        List<CtConstructor<?>> ctors = new ArrayList<>();
        if (t instanceof CtClass<?> cc) ctors.addAll(cc.getConstructors());
        ctors.removeIf(c -> c.getParameters().isEmpty());
        List<CtConstructor<?>> autowired = ctors.stream().filter(c -> hasAnnotationQfn(c, AUTOWIRED)).toList();
        List<CtConstructor<?>> selected;
        if (!autowired.isEmpty()) selected = autowired;
        else if (ctors.size() == 1) selected = ctors;
        else selected = List.of();
        if (ctors.size() > 1 && autowired.isEmpty()) {
            System.out.println("[warn] " + t.getQualifiedName() + " has " + ctors.size()
                    + " constructors, none @Autowired — injection points skipped");
        }
        boolean hasExplicitCtor = !ctors.isEmpty();
        for (CtConstructor<?> c : selected) {
            for (CtParameter<?> p : c.getParameters()) {
                if (hasAnnotationQfn(p, VALUE_ANNO)) continue;
                ips.add(basePoint(p.getType(), p.getSimpleName(), "constructor", p));
            }
        }

        // Lombok @RequiredArgsConstructor: final/@NonNull fields of THIS class (Lombok ignores inherited fields)
        if (!hasExplicitCtor && hasAnnotationQfn(t, LOMBOK_RCA)) {
            for (CtField<?> f : t.getFields()) {
                if (f.isStatic()) continue;
                if (f.isFinal() || hasAnnotationQfn(f, LOMBOK_NONNULL)) {
                    ips.add(basePoint(f.getType(), f.getSimpleName(), "lombok-constructor", f));
                }
            }
        }

        // field injection — own fields, then inherited from superclasses
        Set<String> seenFieldNames = new HashSet<>();
        for (CtField<?> f : t.getFields()) {
            seenFieldNames.add(f.getSimpleName());
            if (f.isStatic() || !hasAnnotationQfn(f, AUTOWIRED)) continue;
            ips.add(basePoint(f.getType(), f.getSimpleName(), "field", f));
        }
        CtTypeReference<?> sup = t.getSuperclass();
        int guard = 0;
        while (sup != null && guard++ < 8) {
            CtType<?> supDecl = sup.getTypeDeclaration();
            if (supDecl == null) break;
            for (CtField<?> f : supDecl.getFields()) {
                if (f.isStatic() || !hasAnnotationQfn(f, AUTOWIRED)) continue;
                if (!seenFieldNames.add(f.getSimpleName())) continue; // overridden/shadowed
                ips.add(basePoint(f.getType(), f.getSimpleName(), "inherited-field", f));
            }
            sup = supDecl.getSuperclass();
        }

        // setter injection
        if (t instanceof CtClass<?> cc) {
            for (CtMethod<?> m : cc.getMethods()) {
                if (m.isStatic() || m.getParameters().size() != 1) continue;
                if (!hasAnnotationQfn(m, AUTOWIRED)) continue;
                CtParameter<?> p = m.getParameters().get(0);
                if (hasAnnotationQfn(p, VALUE_ANNO)) continue;
                ips.add(basePoint(p.getType(), p.getSimpleName(), "setter", p));
            }
        }
        return ips;
    }

    static String ownerBeanKey(CtType<?> t, Map<String, BeanDef> beans) {
        for (Map.Entry<String, BeanDef> e : beans.entrySet()) {
            if (e.getValue().typeQfn.equals(t.getQualifiedName())) return e.getKey();
        }
        return null;
    }

    // ---------- resolution ----------
    static List<BeanDef> resolveCandidates(InjectionPoint ip, Map<String, BeanDef> beans) {
        String wantQfn = qfn(ip.type);
        List<BeanDef> candidates = new ArrayList<>();
        for (BeanDef b : beans.values()) {
            if (b.typeRef == null) continue;
            if (isSubtypeQfn(b.typeRef, wantQfn)) candidates.add(b);
        }
        if (ip.qualifier != null) {
            return candidates.stream().filter(c -> c.beanName.equals(ip.qualifier)).toList();
        }
        if (candidates.size() > 1 && !ip.collection) {   // collection injection takes ALL candidates by design
            List<BeanDef> primaries = candidates.stream().filter(c -> c.isPrimary).toList();
            if (primaries.size() == 1) return primaries;
            if (ip.name != null) {   // Spring byType -> byName fallback
                List<BeanDef> byName = candidates.stream().filter(c -> c.beanName.equals(ip.name)).toList();
                if (byName.size() == 1) return byName;
            }
        }
        return candidates;
    }

    static boolean isSubtypeQfn(CtTypeReference<?> ref, String qfn) {
        Set<String> visited = new HashSet<>();
        Deque<CtTypeReference<?>> work = new ArrayDeque<>();
        work.add(ref);
        while (!work.isEmpty()) {
            CtTypeReference<?> r = work.poll();
            if (r == null) continue;
            String q = r.getQualifiedName();
            if (q == null || !visited.add(q)) continue;
            if (q.equals(qfn)) return true;
            if (visited.size() > 200) return false;
            CtType<?> decl = r.getTypeDeclaration();
            if (decl == null) continue;
            if (decl.getSuperclass() != null) work.add(decl.getSuperclass());
            work.addAll(decl.getSuperInterfaces());
        }
        return false;
    }

    // ---------- helpers ----------
    static boolean hasAnnotationQfn(CtElement el, String qfn) {
        for (CtAnnotation<?> a : el.getAnnotations()) {
            if (qfn.equals(a.getAnnotationType().getQualifiedName())) return true;
        }
        return false;
    }

    static String paramQualifier(CtParameter<?> p) {
        for (CtAnnotation<?> a : p.getAnnotations()) {
            if (QUALIFIER.equals(a.getAnnotationType().getQualifiedName())) {
                String v = annoValue(a, "value");
                if (v != null) return v;
            }
        }
        return null;
    }

    static String fieldQualifier(CtField<?> f) {
        for (CtAnnotation<?> a : f.getAnnotations()) {
            if (QUALIFIER.equals(a.getAnnotationType().getQualifiedName())) {
                String v = annoValue(a, "value");
                if (v != null) return v;
            }
        }
        return null;
    }

    static String annoValue(CtAnnotation<?> a, String name) {
        try {
            Object v = a.getValueAsObject(name);
            if (v == null) return null;
            if (v.getClass().isArray()) {   // String[]/Class[] attributes (e.g. @ControllerAdvice.value default)
                int n = java.lang.reflect.Array.getLength(v);
                if (n == 0) return null;    // empty default = "no explicit value"
                StringBuilder sb = new StringBuilder();
                for (int i = 0; i < n; i++) {
                    Object o = java.lang.reflect.Array.get(v, i);
                    sb.append(o == null ? "" : o.toString());
                }
                return sb.isEmpty() ? null : sb.toString();
            }
            if (v instanceof Collection<?> c) {
                StringBuilder sb = new StringBuilder();
                for (Object o : c) sb.append(o == null ? "" : o.toString());
                return sb.isEmpty() ? null : sb.toString();
            }
            String s = v.toString();
            if (s.startsWith("\"") && s.endsWith("\"") && s.length() >= 2) s = s.substring(1, s.length() - 1);
            return s.isEmpty() ? null : s;
        } catch (Exception e) {
            return null;
        }
    }

    static String decapitalize(String s) {
        if (s.length() > 1 && Character.isUpperCase(s.charAt(0)) && Character.isUpperCase(s.charAt(1))) return s;
        return Character.toLowerCase(s.charAt(0)) + s.substring(1);
    }

    static String qfn(CtTypeReference<?> r) { return r == null ? "?" : r.getQualifiedName(); }

    static String posFile(CtElement e) {
        try { return e.getPosition().getFile() == null ? "?" : e.getPosition().getFile().getName(); }
        catch (Exception ex) { return "?"; }
    }

    static int posLine(CtElement e) {
        try { return e.getPosition().getLine(); } catch (Exception ex) { return -1; }
    }
}
