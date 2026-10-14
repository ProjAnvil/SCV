package projanvil.scv.beanprobe;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Stream;

import static org.junit.jupiter.api.Assertions.*;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

/** Ground-truth regression: fixtures live in src/test/resources/fixtures and are
 *  copied to a temp dir so nested gradle builds never touch the source tree. */
class PerceptionRegressionTest {

    @TempDir
    Path temp;

    private static boolean gradleAvailable() {
        try {
            Process p = new ProcessBuilder("gradle", "--version").redirectErrorStream(true).start();
            p.getInputStream().readAllBytes();
            return p.waitFor() == 0;
        } catch (Exception e) {
            return false;
        }
    }

    private Path runProbe(String fixture) throws Exception {
        assumeTrue(gradleAvailable(), "gradle not on PATH — regression self-skips");
        Path fixtureDir = copyFixture(fixture);
        Path out = temp.resolve(fixture + ".json");
        String bin = Path.of("build", "install", "springboot-probe", "bin", "springboot-probe").toAbsolutePath().toString();
        Process p = new ProcessBuilder(bin, "gradle", fixtureDir.toString(), out.toString())
                .redirectErrorStream(true).start();
        try (var sc = new java.util.Scanner(p.getInputStream())) {
            while (sc.hasNextLine()) System.out.println("[probe] " + sc.nextLine());
        }
        assertEquals(0, p.waitFor(), "probe exited non-zero");
        return out;
    }

    private Path copyFixture(String name) throws Exception {
        Path src = Path.of("src", "test", "resources", "fixtures", name);
        Path dst = temp.resolve(name);
        try (Stream<Path> walk = Files.walk(src)) {
            for (Path f : walk.filter(Files::isRegularFile).toList()) {
                Path target = dst.resolve(src.relativize(f).toString());
                Files.createDirectories(target.getParent());
                Files.copy(f, target);
            }
        }
        return dst;
    }

    private static List<?> array(String json, String key) throws Exception {
        // gson 2.13: JsonArray implements Iterable, not java.util.List — use asList()
        return ((com.google.gson.JsonArray) rawJson(json, key)).asList();
    }

    private static int count(String json, String key) throws Exception {
        return ((com.google.gson.JsonPrimitive) rawJson(json, key)).getAsInt();
    }

    private static Object rawJson(String json, String key) throws Exception {
        return com.google.gson.JsonParser.parseString(json).getAsJsonObject().get(key);
    }

    @Test
    void lombokSampleGroundTruth() throws Exception {
        String json = Files.readString(runProbe("gradle-lombok-sample"));
        assertEquals(12, array(json, "nodes").size(), "beans");
        assertEquals(8, array(json, "edges").size(), "edges");
        assertEquals(0, count(json, "unresolved"), "no unresolved injections");
        String flat = json.replaceAll("\\s+", ""); // gson pretty-print emits "key": value
        assertTrue(flat.contains("\"ambiguous\":true"), "ambiguous pair flagged");
        assertTrue(flat.contains("\"qualifier\":\"emailSender\""), "qualifier edge present");
    }

    @Test
    void tortureAppGroundTruth() throws Exception {
        String json = Files.readString(runProbe("torture-app"));
        assertEquals(30, array(json, "nodes").size(), "beans");
        assertEquals(24, array(json, "edges").size(), "edges");
        String flat = json.replaceAll("\\s+", "");
        assertTrue(flat.contains("\"via\":\"inherited-field\""), "inherited injection edge");
        assertTrue(flat.contains("\"via\":\"beanMethod\""), "@Bean param edges");
        assertTrue(json.contains("bean:circularA") && json.contains("bean:circularB"), "cycle pair present");
        assertEquals(0, count(json, "unresolved"), "no unresolved injections");
    }

    @Test
    void boot2LegacyGroundTruth() throws Exception {
        // Spring Boot 2.7: javax lifecycle, field injection, @Autowired multi-ctor,
        // 1-level inheritance, competing @Profile beans, @Value mixed ctor.
        String json = Files.readString(runProbe("boot2-legacy"));
        assertEquals(15, array(json, "nodes").size(), "beans");
        assertEquals(10, array(json, "edges").size(), "edges");
        assertEquals(0, count(json, "unresolved"), "no unresolved injections");
        String flat = json.replaceAll("\\s+", "");
        assertTrue(flat.contains("\"via\":\"inherited-field\""), "injected field from abstract controller");
        assertTrue(flat.contains("bean:notifierService\""), "multi-ctor: only @Autowired ctor counted");
        long ambiguous = flat.split("\"ambiguous\":true", -1).length - 1;
        assertEquals(4, ambiguous, "two ambiguous pairs (channel + greeting profiles)");
        assertTrue(flat.contains("bean:mockGreetingService") && flat.contains("bean:realGreetingService"),
                "competing profile beans present");
    }

    @Test
    void boot3ModernGroundTruth() throws Exception {
        // Spring Boot 3.5 / Java 21: sealed interface + record beans, record
        // constructor binding of a record @ConfigurationProperties, @Primary
        // collapse, jakarta lifecycle, switch expressions in bodies.
        String json = Files.readString(runProbe("boot3-modern"));
        assertEquals(12, array(json, "nodes").size(), "beans");
        assertEquals(6, array(json, "edges").size(), "edges");
        assertEquals(0, count(json, "unresolved"), "no unresolved injections");
        String flat = json.replaceAll("\\s+", "");
        assertTrue(flat.contains("bean:paymentProps"), "record @ConfigurationProperties registered");
        assertTrue(flat.contains("\"stereotype\":\"ConfigurationProperties\""),
                "properties record carries the ConfigurationProperties stereotype");
        assertTrue(flat.contains("bean:cardChannel") && flat.contains("bean:paymentProps"),
                "record constructor binding edge cardChannel -> paymentProps");
        long amb = flat.split("\"ambiguous\":true", -1).length - 1;
        assertEquals(0, amb, "@Primary collapses the sealed-interface pair");
        assertTrue(flat.contains("bean:asyncConfig"), "@Import'ed @Configuration detected");
    }

    @Test
    void boot2MultiGroundTruth() throws Exception {
        // Spring Boot 2.7 multi-module with the boot entry point in the web
        // submodule: multi-module detection + cross-module DI edges.
        String json = Files.readString(runProbe("boot2-multi"));
        assertEquals(6, array(json, "nodes").size(), "beans");
        assertEquals(4, array(json, "edges").size(), "edges");
        assertEquals(0, count(json, "unresolved"), "no unresolved injections");
        String flat = json.replaceAll("\\s+", "");
        assertTrue(flat.contains("bean:boot2MultiApplication"), "submodule @SpringBootApplication detected");
        assertTrue(flat.contains("bean:customerController") && flat.contains("bean:customerRepository"),
                "web -> domain cross-module components present");
    }
}
