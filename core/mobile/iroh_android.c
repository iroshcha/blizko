//go:build android && iroh

#include <jni.h>
#include <pthread.h>

void blizko_iroh_android_context(void *vm, void *context);

// Initialized from Application.onCreate before any iroh endpoint is started.
// A global Application reference is retained deliberately for the VM lifetime.
JNIEXPORT void JNICALL Java_ru_blizko_chat_ChatApp_initializeIroh(JNIEnv *env, jclass cls, jobject context) {
    static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;
    static jobject application = NULL;
    (void)cls;
    pthread_mutex_lock(&lock);
    if (application == NULL) {
        JavaVM *vm = NULL;
        if ((*env)->GetJavaVM(env, &vm) == JNI_OK) {
            application = (*env)->NewGlobalRef(env, context);
            if (application != NULL) blizko_iroh_android_context(vm, application);
        }
    }
    pthread_mutex_unlock(&lock);
}
